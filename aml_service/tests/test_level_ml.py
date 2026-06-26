"""
Unit tests for levels/level_ml.py (MLLevel) and train.py utilities.

Strategy
--------
MLLevel depends on:
  - A TransactionsGraph instance  (we provide real ones)
  - A .joblib artifact file       (we mock joblib.load)
  - GargIndex                     (constructed internally; we let it run on
                                   tiny graphs so it stays fast)

We test the *orchestration logic*:
  - Feature caching (features computed once, not on every call)
  - Missing-feature backfilling with 0
  - Threshold comparison producing (bool, float) tuples
  - predict_all returning one entry per account
  - update_graph resetting cached state
  - predict_account on unknown account returns 0

train.py get_features() is a pure DataFrame transformation — we test it
directly without any mocking.

Run with:
    pytest test_level_ml.py -v
"""

import json
import os
import pytest
import numpy as np
import pandas as pd
import networkx as nx
from unittest.mock import patch, MagicMock, mock_open

from graph.builder import TransactionsGraph


# ---------------------------------------------------------------------------
# Shared thresholds mock (level_ml imports thresholds.json at module level)
# ---------------------------------------------------------------------------

MOCK_THRESHOLDS = {
    "personal": {
        "tx_threshold": 10_000,
        "daily_limit": 50_000,
        "max_fan_out": 5,
        "max_fan_in": 5,
        "output_money_amount_check_cycles": 1_000,
        "cycle_amount_percentage": 0.5,
        "scatter_accounts_threshold": 3,
        "gather_accounts_threshold": 3,
        "gather_scatter_money_ratio": 2.0,
    }
}


# ---------------------------------------------------------------------------
# Minimal stub model
# ---------------------------------------------------------------------------

class _StubModel:
    """Mimics sklearn's predict_proba interface."""

    def __init__(self, proba=0.8):
        self._proba = proba

    def predict_proba(self, X):
        n = len(X)
        return np.array([[1 - self._proba, self._proba]] * n)


def _make_artifact(proba=0.8, threshold=0.5, features=None):
    if features is None:
        features = [
            "garg_index", "Cycled_Money", "Input_Money", "Output_Money",
            "Input_Output_Ratio", "Cycled_Input_Ratio", "Cycled_Output_Ratio",
            "indegree", "outdegree",
            "Transactions_Out", "Transactions_In",
            "Avg_TX_in", "Avg_TX_out",
            "Transactions_Per_Input", "Transactions_Per_Output",
        ]
    return {
        "model": _StubModel(proba),
        "threshold": threshold,
        "features": features,
    }


# ---------------------------------------------------------------------------
# Graph factory — small but realistic enough for GargIndex
# ---------------------------------------------------------------------------

def _small_graph():
    """
    A → B → C, A → C
    Three nodes, enough edges so GargIndex doesn't get an empty graph.
    """
    g = TransactionsGraph()
    g.add_transaction("A", "B", 500, 1)
    g.add_transaction("B", "C", 300, 2)
    g.add_transaction("A", "C", 200, 3)
    return g


# ---------------------------------------------------------------------------
# Import MLLevel with all file I/O patched
# ---------------------------------------------------------------------------

with (
    patch("builtins.open", mock_open(read_data=json.dumps(MOCK_THRESHOLDS))),
    patch("joblib.load", return_value=_make_artifact()),
):
    from levels.level_ml import MLLevel


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def ml(monkeypatch):
    """
    Return an MLLevel backed by a small, real TransactionsGraph.
    joblib.load is patched for every instantiation in each test.
    """
    artifact = _make_artifact()
    with patch("joblib.load", return_value=artifact):
        instance = MLLevel(_small_graph())
    return instance


# ============================================================
# 1. Construction
# ============================================================

class TestMLLevelConstruction:

    def test_model_loaded(self):
        artifact = _make_artifact()
        with patch("joblib.load", return_value=artifact):
            m = MLLevel(_small_graph())
        assert m.model is artifact["model"]

    def test_threshold_loaded(self):
        artifact = _make_artifact(threshold=0.3)
        with patch("joblib.load", return_value=artifact):
            m = MLLevel(_small_graph())
        assert m.threshold == 0.3

    def test_features_initially_none(self):
        artifact = _make_artifact()
        with patch("joblib.load", return_value=artifact):
            m = MLLevel(_small_graph())
        assert m.features is None


# ============================================================
# 2. predict_account — basic behaviour
# ============================================================

class TestPredictAccount:

    def test_known_account_returns_tuple(self, ml):
        result = ml.predict_account("A")
        assert isinstance(result, tuple)
        is_fraud, proba = result
        assert isinstance(is_fraud, (bool, np.bool_))
        assert 0.0 <= proba <= 1.0

    def test_unknown_account_returns_zero(self, ml):
        result = ml.predict_account("GHOST_ACCOUNT_XYZ")
        assert result == 0

    def test_above_threshold_flagged_as_fraud(self):
        """Model always returns proba=0.9; threshold=0.5 → should be flagged."""
        artifact = _make_artifact(proba=0.9, threshold=0.5)
        with patch("joblib.load", return_value=artifact):
            m = MLLevel(_small_graph())
        is_fraud, proba = m.predict_account("A")
        assert is_fraud is True
        assert abs(proba - 0.9) < 1e-6

    def test_below_threshold_not_flagged(self):
        """Model always returns proba=0.2; threshold=0.5 → should not be flagged."""
        artifact = _make_artifact(proba=0.2, threshold=0.5)
        with patch("joblib.load", return_value=artifact):
            m = MLLevel(_small_graph())
        is_fraud, proba = m.predict_account("A")
        assert is_fraud is False
        assert abs(proba - 0.2) < 1e-6

    def test_result_stored_in_scores(self, ml):
        ml.predict_account("A")
        assert "A" in ml.scores

    def test_features_cached_after_first_call(self, ml):
        ml.predict_account("A")
        features_after_first = ml.features
        assert features_after_first is not None
        ml.predict_account("B")
        # Same object — not recomputed
        assert ml.features is features_after_first


# ============================================================
# 3. Missing feature backfill
# ============================================================

class TestMissingFeatureBackfill:

    def test_missing_features_filled_with_zero(self):
        """
        Request a feature that doesn't exist in the computed DataFrame.
        MLLevel should silently add it as 0 and not crash.
        """
        extra = "totally_made_up_feature_xyz"
        base_features = [
            "garg_index", "Cycled_Money", "Input_Money", "Output_Money",
            "Input_Output_Ratio", "Cycled_Input_Ratio", "Cycled_Output_Ratio",
            "indegree", "outdegree",
            "Transactions_Out", "Transactions_In",
            "Avg_TX_in", "Avg_TX_out",
            "Transactions_Per_Input", "Transactions_Per_Output",
        ]
        artifact = _make_artifact(features=base_features + [extra])
        with patch("joblib.load", return_value=artifact):
            m = MLLevel(_small_graph())
        # Should not raise
        result = m.predict_account("A")
        assert isinstance(result, tuple)


# ============================================================
# 4. predict_all
# ============================================================

class TestPredictAll:

    def test_predict_all_returns_dict(self, ml):
        result = ml.predict_all()
        assert isinstance(result, dict)

    def test_predict_all_covers_all_accounts(self, ml):
        result = ml.predict_all()
        for account in ["A", "B", "C"]:
            assert account in result

    def test_predict_all_values_are_tuples(self, ml):
        result = ml.predict_all()
        for account, val in result.items():
            assert isinstance(val, tuple), f"{account}: expected tuple, got {type(val)}"
            is_fraud, proba = val
            assert 0.0 <= proba <= 1.0

    def test_predict_all_populates_scores(self, ml):
        ml.predict_all()
        assert len(ml.scores) > 0

    def test_predict_all_uses_cached_features(self, ml):
        # Compute features via predict_account first
        ml.predict_account("A")
        features_ref = ml.features
        # predict_all should reuse the same object
        ml.predict_all()
        assert ml.features is features_ref

    def test_predict_all_consistent_with_predict_account(self, ml):
        """predict_all and predict_account must agree on the same graph state."""
        all_results = ml.predict_all()
        for account in ["A", "B", "C"]:
            if account in all_results:
                _, proba_all = all_results[account]
                # Re-create to get a fresh instance with same artifact
                artifact = _make_artifact()
                with patch("joblib.load", return_value=artifact):
                    m2 = MLLevel(_small_graph())
                _, proba_single = m2.predict_account(account)
                assert abs(proba_all - proba_single) < 1e-6, (
                    f"Proba mismatch for {account}: {proba_all} vs {proba_single}"
                )


# ============================================================
# 5. update_graph
# ============================================================

class TestUpdateGraph:

    def test_update_graph_resets_features(self, ml):
        ml.predict_account("A")          # populates features
        assert ml.features is not None
        ml.update_graph(_small_graph())
        assert ml.features is None

    def test_update_graph_resets_scores(self, ml):
        ml.predict_account("A")
        assert len(ml.scores) > 0
        ml.update_graph(_small_graph())
        assert len(ml.scores) == 0

    def test_predict_after_update_works(self, ml):
        ml.predict_account("A")
        new_g = _small_graph()
        new_g.add_transaction("A", "B", 999, 10)
        ml.update_graph(new_g)
        # Should recompute features on the updated graph without error
        result = ml.predict_account("A")
        assert isinstance(result, tuple)

    def test_update_replaces_graph_reference(self, ml):
        new_g = _small_graph()
        ml.update_graph(new_g)
        assert ml.graph is new_g


# ============================================================
# 6. get_communities
# ============================================================

class TestGetCommunities:

    def test_communities_initially_none(self, ml):
        """Before any score is computed, communities attribute on garg is None."""
        assert ml.garg.communities is None

    def test_communities_populated_after_prediction(self, ml):
        ml.predict_account("A")
        # GargIndex lazily computes communities on first score request
        assert ml.garg.communities is not None

    def test_get_communities_returns_list(self, ml):
        ml.predict_account("A")   # trigger community computation
        communities = ml.get_communities()
        assert isinstance(communities, list)


# ============================================================
# 7. train.py — get_features() pure transformation
# ============================================================

class TestGetFeatures:
    """
    get_features() takes a DataFrame with Account_ID, Input_Money, Output_Money,
    Cycled_Money columns and a graph, and appends derived ratio/degree features.
    """

    @pytest.fixture
    def setup(self):
        from train import get_features
        g = _small_graph()
        df = pd.DataFrame({
            "Account_ID": ["A", "B", "C"],
            "Input_Money": [200.0, 500.0, 500.0],
            "Output_Money": [700.0, 300.0, 0.0],
            "Cycled_Money": [0.0, 0.0, 0.0],
        })
        result = get_features(df, g)
        return result, g

    def test_expected_columns_added(self, setup):
        result, _ = setup
        expected = [
            "Input_Output_Ratio", "Cycled_Input_Ratio", "Cycled_Output_Ratio",
            "indegree", "outdegree",
            "Transactions_Out", "Transactions_In",
            "Avg_TX_in", "Avg_TX_out",
            "Transactions_Per_Input", "Transactions_Per_Output",
        ]
        for col in expected:
            assert col in result.columns, f"Missing column: {col}"

    def test_input_output_ratio_correct(self, setup):
        result, _ = setup
        epsilon = 1e-9
        row_A = result[result["Account_ID"] == "A"].iloc[0]
        expected = row_A["Input_Money"] / (row_A["Output_Money"] + epsilon)
        assert abs(row_A["Input_Output_Ratio"] - expected) < 1e-6

    def test_no_division_by_zero_when_output_zero(self, setup):
        """C has Output_Money=0; ratio columns must be finite."""
        result, _ = setup
        row_C = result[result["Account_ID"] == "C"].iloc[0]
        assert np.isfinite(row_C["Input_Output_Ratio"])
        assert np.isfinite(row_C["Cycled_Input_Ratio"])

    def test_degree_columns_are_non_negative(self, setup):
        result, _ = setup
        for col in ["indegree", "outdegree", "Transactions_In", "Transactions_Out"]:
            assert (result[col] >= 0).all(), f"Negative values in {col}"

    def test_unknown_account_degree_is_zero(self):
        """An account not in the graph should get degree=0, not crash."""
        from train import get_features
        g = _small_graph()
        df = pd.DataFrame({
            "Account_ID": ["GHOST"],
            "Input_Money": [0.0],
            "Output_Money": [0.0],
            "Cycled_Money": [0.0],
        })
        result = get_features(df, g)
        assert result["indegree"].iloc[0] == 0
        assert result["outdegree"].iloc[0] == 0

    def test_row_count_preserved(self, setup):
        result, _ = setup
        assert len(result) == 3

    def test_avg_tx_in_is_input_divided_by_transactions_in(self, setup):
        result, _ = setup
        epsilon = 1e-9
        for _, row in result.iterrows():
            expected = row["Input_Money"] / (row["Transactions_In"] + epsilon)
            assert abs(row["Avg_TX_in"] - expected) < 1e-6

    def test_transactions_per_input_is_transactions_in_divided_by_indegree(self, setup):
        result, _ = setup
        epsilon = 1e-9
        for _, row in result.iterrows():
            expected = row["Transactions_In"] / (row["indegree"] + epsilon)
            assert abs(row["Transactions_Per_Input"] - expected) < 1e-6