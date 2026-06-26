"""
Integration tests for main.py (FastAPI AML service)

Strategy
--------
We use FastAPI's TestClient (Starlette's test harness) to make real HTTP
requests against the app without starting a server process.

The level functions (level_1, level_2, ML) are mocked at the point where
main.py imports them — this keeps tests fast and deterministic while still
exercising the routing, response-shaping, and global-state logic in main.py.

Global state cleanup
--------------------
main.py holds two module-level globals:
  - `ml_level_instance`  (None until first /check_accounts call)
  - `account_types`      (dict populated by /check_transaction calls)

The `clean_globals` fixture resets both before every test.

Run with:
    pytest test_main.py -v
"""

import json
import pytest
from datetime import datetime
from unittest.mock import MagicMock, patch
from fastapi.testclient import TestClient

# ---------------------------------------------------------------------------
# Import the app — all heavy module-level I/O in level_*.py is already
# handled by those modules' own import-time patches (in their test files).
# Here we only need to patch the *functions* that main.py calls.
# ---------------------------------------------------------------------------

import json
from unittest.mock import patch, mock_open, MagicMock

MOCK_THRESHOLDS = {
    "personal": {
        "tx_threshold": 10_000, "daily_limit": 50_000,
        "max_fan_out": 5, "max_fan_in": 5,
        "output_money_amount_check_cycles": 1_000,
        "cycle_amount_percentage": 0.5,
        "scatter_accounts_threshold": 3, "gather_accounts_threshold": 3,
        "gather_scatter_money_ratio": 2.0,
    }
}

# Patch file opens and joblib.load before importing main so module-level
# code in level_rules / level_graph / level_ml doesn't crash.
with (
    patch("builtins.open", mock_open(read_data=json.dumps(MOCK_THRESHOLDS))),
    patch("joblib.load", return_value={
        "model": MagicMock(**{"predict_proba.return_value": [[0.3, 0.7]]}),
        "threshold": 0.5,
        "features": ["garg_index"],
    }),
):
    from main import app
    import main as main_module


# ---------------------------------------------------------------------------
# TestClient
# ---------------------------------------------------------------------------

client = TestClient(app, raise_server_exceptions=False)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _tx_payload(
    sender="alice",
    receiver="bob",
    amount=500.0,
    sender_account_type="personal",
    receiver_account_type="personal",
    timestamp="2025-01-01T12:00:00",
):
    return {
        "sender": sender,
        "receiver": receiver,
        "amount": amount,
        "timestamp": timestamp,
        "sender_account_type": sender_account_type,
        "receiver_account_type": receiver_account_type,
    }


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(autouse=True)
def clean_globals():
    """Reset module-level globals before and after every test."""
    main_module.ml_level_instance = None
    main_module.account_types.clear()
    yield
    main_module.ml_level_instance = None
    main_module.account_types.clear()


# ============================================================
# 1. POST /check_transaction — self-loop guard
# ============================================================

class TestCheckTransactionSelfLoop:

    def test_same_sender_receiver_rejected(self):
        resp = client.post("/check_transaction", json=_tx_payload(sender="alice", receiver="alice"))
        assert resp.status_code == 200
        body = resp.json()
        assert body["status"] == "rejected"
        assert "same" in body["reason"].lower()

    def test_self_loop_does_not_call_level_1(self):
        with patch("main.level_1_check_transaction") as mock_l1:
            client.post("/check_transaction", json=_tx_payload(sender="X", receiver="X"))
            mock_l1.assert_not_called()

    def test_self_loop_does_not_call_level_2_add(self):
        with patch("main.level_2_add_transaction") as mock_add:
            client.post("/check_transaction", json=_tx_payload(sender="X", receiver="X"))
            mock_add.assert_not_called()


# ============================================================
# 2. POST /check_transaction — level 1 pass → approved
# ============================================================

class TestCheckTransactionApproved:

    def test_approved_returns_approved_status(self):
        with (
            patch("main.level_1_check_transaction", return_value=(True, "")),
            patch("main.level_2_add_transaction", return_value=(True, "")),
        ):
            resp = client.post("/check_transaction", json=_tx_payload())
        assert resp.status_code == 200
        assert resp.json()["status"] == "approved"

    def test_approved_calls_level_2_add(self):
        with (
            patch("main.level_1_check_transaction", return_value=(True, "")),
            patch("main.level_2_add_transaction") as mock_add,
        ):
            client.post("/check_transaction", json=_tx_payload())
            mock_add.assert_called_once()

    def test_approved_populates_account_types(self):
        with (
            patch("main.level_1_check_transaction", return_value=(True, "")),
            patch("main.level_2_add_transaction", return_value=(True, "")),
        ):
            client.post("/check_transaction", json=_tx_payload(
                sender="alice", receiver="bob",
                sender_account_type="personal", receiver_account_type="personal",
            ))
        assert main_module.account_types["alice"] == "personal"
        assert main_module.account_types["bob"] == "personal"

    def test_approved_response_shape(self):
        with (
            patch("main.level_1_check_transaction", return_value=(True, "")),
            patch("main.level_2_add_transaction", return_value=(True, "")),
        ):
            resp = client.post("/check_transaction", json=_tx_payload())
        body = resp.json()
        assert "status" in body
        assert "reason" in body


# ============================================================
# 3. POST /check_transaction — level 1 fail → rejected
# ============================================================

class TestCheckTransactionRejected:

    def test_rejected_returns_rejected_status(self):
        with patch("main.level_1_check_transaction", return_value=(False, "amount too high")):
            resp = client.post("/check_transaction", json=_tx_payload())
        assert resp.status_code == 200
        assert resp.json()["status"] == "rejected"

    def test_rejected_reason_forwarded(self):
        with patch("main.level_1_check_transaction", return_value=(False, "amount too high")):
            resp = client.post("/check_transaction", json=_tx_payload())
        assert "amount too high" in resp.json()["reason"]

    def test_rejected_does_not_call_level_2_add(self):
        with (
            patch("main.level_1_check_transaction", return_value=(False, "blocked")),
            patch("main.level_2_add_transaction") as mock_add,
        ):
            client.post("/check_transaction", json=_tx_payload())
            mock_add.assert_not_called()

    def test_rejected_still_populates_account_types(self):
        """account_types is updated before the level checks run."""
        with patch("main.level_1_check_transaction", return_value=(False, "blocked")):
            client.post("/check_transaction", json=_tx_payload(
                sender="carol", sender_account_type="personal"
            ))
        assert "carol" in main_module.account_types


# ============================================================
# 4. POST /check_transaction — internal exception handling
# ============================================================

class TestCheckTransactionException:

    def test_exception_returns_internal_error(self):
        with patch("main.level_1_check_transaction", side_effect=RuntimeError("boom")):
            resp = client.post("/check_transaction", json=_tx_payload())
        body = resp.json()
        assert body["status"] == "INTERNAL_ERROR"
        assert "boom" in body["reason"]

    def test_exception_does_not_propagate_as_500(self):
        with patch("main.level_1_check_transaction", side_effect=RuntimeError("boom")):
            resp = client.post("/check_transaction", json=_tx_payload())
        assert resp.status_code == 200   # caught internally


# ============================================================
# 5. POST /check_accounts — empty graph path
# ============================================================

class TestCheckAccountsEmptyGraph:

    def _make_empty_graph(self):
        g = MagicMock()
        g.get_accounts.return_value = []
        g.clean_edges.return_value = None
        return g

    def test_empty_graph_returns_success(self):
        with patch("main.level_2_get_graph", return_value=self._make_empty_graph()):
            resp = client.post("/check_accounts")
        assert resp.json()["status"] == "success"

    def test_empty_graph_returns_empty_lists(self):
        with patch("main.level_2_get_graph", return_value=self._make_empty_graph()):
            resp = client.post("/check_accounts")
        body = resp.json()
        assert body["level_2"] == []
        assert body["level_3"] == []

    def test_empty_graph_ml_not_instantiated(self):
        with patch("main.level_2_get_graph", return_value=self._make_empty_graph()):
            client.post("/check_accounts")
        assert main_module.ml_level_instance is None


# ============================================================
# 6. POST /check_accounts — ML instance lifecycle
# ============================================================

class TestCheckAccountsMLLifecycle:

    def _make_graph_with_accounts(self, accounts=("alice", "bob")):
        g = MagicMock()
        g.get_accounts.return_value = list(accounts)
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False
        return g

    def _make_ml(self, accounts=("alice", "bob")):
        ml = MagicMock()
        ml.predict_all.return_value = {a: (False, 0.1) for a in accounts}
        ml.get_communities.return_value = []
        return ml

    def test_ml_instance_created_on_first_call(self):
        assert main_module.ml_level_instance is None
        g = self._make_graph_with_accounts()
        ml = self._make_ml()
        for acc in ["alice", "bob"]:
            main_module.account_types[acc] = "personal"

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml) as MockML,
        ):
            client.post("/check_accounts")
            MockML.assert_called_once_with(g)

        assert main_module.ml_level_instance is ml

    def test_ml_instance_updated_on_subsequent_calls(self):
        ml = self._make_ml()
        main_module.ml_level_instance = ml   # pre-set
        g = self._make_graph_with_accounts()
        for acc in ["alice", "bob"]:
            main_module.account_types[acc] = "personal"

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
        ):
            client.post("/check_accounts")

        ml.update_graph.assert_called_once_with(g)

    def test_ml_constructor_not_called_on_second_request(self):
        ml = self._make_ml()
        main_module.ml_level_instance = ml
        g = self._make_graph_with_accounts()
        for acc in ["alice", "bob"]:
            main_module.account_types[acc] = "personal"

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel") as MockML,
        ):
            client.post("/check_accounts")
            MockML.assert_not_called()


# ============================================================
# 7. POST /check_accounts — level 2 flagging
# ============================================================

class TestCheckAccountsLevel2:

    def _setup(self, accounts, account_check_results, communities=None):
        """
        account_check_results: dict {account: (ok, reason)}
        """
        g = MagicMock()
        g.get_accounts.return_value = list(accounts)
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False

        ml = MagicMock()
        ml.predict_all.return_value = {a: (False, 0.1) for a in accounts}
        ml.get_communities.return_value = communities or []

        for acc in accounts:
            main_module.account_types[acc] = "personal"

        def fake_check_account(account, account_type):
            return account_check_results.get(account, (True, ""))

        return g, ml, fake_check_account

    def test_clean_account_not_in_level_2_results(self):
        accounts = ["alice", "bob"]
        results = {"alice": (True, ""), "bob": (True, "")}
        g, ml, checker = self._setup(accounts, results)

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", side_effect=checker),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        flagged = {item["account"] for item in resp.json()["level_2"]}
        assert "alice" not in flagged
        assert "bob" not in flagged

    def test_flagged_account_appears_in_level_2_results(self):
        accounts = ["alice", "bob"]
        results = {"alice": (False, "fan out exceeded"), "bob": (True, "")}
        g, ml, checker = self._setup(accounts, results)

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", side_effect=checker),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        flagged = {item["account"]: item["reason"] for item in resp.json()["level_2"]}
        assert "alice" in flagged
        assert "fan out exceeded" in flagged["alice"]

    def test_account_missing_from_account_types_defaults_to_PERSON(self):
        """Accounts that slipped through without a type should default to PERSON."""
        accounts = ["unknown_acc"]
        # Don't pre-populate account_types for this account
        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False

        ml = MagicMock()
        ml.predict_all.return_value = {"unknown_acc": (False, 0.1)}
        ml.get_communities.return_value = []

        captured_types = {}

        def capturing_check(account, account_type):
            captured_types[account] = account_type
            return True, ""

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", side_effect=capturing_check),
            patch("main.MLLevel", return_value=ml),
        ):
            client.post("/check_accounts")

        assert captured_types.get("unknown_acc") == "PERSON"


# ============================================================
# 8. POST /check_accounts — level 3 (ML) flagging
# ============================================================

class TestCheckAccountsLevel3:

    def test_ml_flagged_account_in_level_3_results(self):
        accounts = ["alice", "bob"]
        for acc in accounts:
            main_module.account_types[acc] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False

        ml = MagicMock()
        ml.predict_all.return_value = {
            "alice": (True, 0.92),   # flagged
            "bob": (False, 0.1),     # clean
        }
        ml.get_communities.return_value = []

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        l3 = resp.json()["level_3"]
        flagged_accounts = {item["account"] for item in l3}
        assert "alice" in flagged_accounts
        assert "bob" not in flagged_accounts

    def test_ml_score_included_in_response(self):
        accounts = ["alice"]
        main_module.account_types["alice"] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False

        ml = MagicMock()
        ml.predict_all.return_value = {"alice": (True, 0.87)}
        ml.get_communities.return_value = []

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        item = next(i for i in resp.json()["level_3"] if i["account"] == "alice")
        assert abs(item["score"] - 0.87) < 1e-6


# ============================================================
# 9. POST /check_accounts — bipartite community detection
# ============================================================

class TestCheckAccountsBipartite:

    def test_bipartite_community_flagged_in_level_2(self):
        accounts = ["A", "B", "C", "D", "E"]
        for acc in accounts:
            main_module.account_types[acc] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        # Community is bipartite
        g.check_bipartite_subgraph.return_value = True

        ml = MagicMock()
        ml.predict_all.return_value = {a: (False, 0.1) for a in accounts}
        ml.get_communities.return_value = [accounts]   # one community = all accounts

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        flagged = {item["account"] for item in resp.json()["level_2"]}
        assert flagged == set(accounts)

    def test_bipartite_reason_in_response(self):
        accounts = ["A", "B", "C"]
        for acc in accounts:
            main_module.account_types[acc] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = True

        ml = MagicMock()
        ml.predict_all.return_value = {a: (False, 0.1) for a in accounts}
        ml.get_communities.return_value = [accounts]

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        for item in resp.json()["level_2"]:
            assert "bipartite" in item["reason"].lower()

    def test_non_bipartite_community_not_flagged(self):
        accounts = ["A", "B", "C"]
        for acc in accounts:
            main_module.account_types[acc] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False   # not bipartite

        ml = MagicMock()
        ml.predict_all.return_value = {a: (False, 0.1) for a in accounts}
        ml.get_communities.return_value = [accounts]

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        assert resp.json()["level_2"] == []

    def test_already_flagged_by_level_2_not_duplicated(self):
        """
        An account already flagged by level_2_check_account must not get a
        duplicate entry when the bipartite check also fires.
        """
        accounts = ["A", "B"]
        for acc in accounts:
            main_module.account_types[acc] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = accounts
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = True

        ml = MagicMock()
        ml.predict_all.return_value = {a: (False, 0.1) for a in accounts}
        ml.get_communities.return_value = [accounts]

        # "A" is already flagged by check_account
        def check_side(account, account_type):
            if account == "A":
                return False, "fan out exceeded"
            return True, ""

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", side_effect=check_side),
            patch("main.MLLevel", return_value=ml),
        ):
            resp = client.post("/check_accounts")

        flagged = [item for item in resp.json()["level_2"] if item["account"] == "A"]
        # Must appear exactly once
        assert len(flagged) == 1


# ============================================================
# 10. POST /check_accounts — account_types cleanup
# ============================================================

class TestCheckAccountsAccountTypeCleanup:

    def test_stale_accounts_removed_from_account_types(self):
        """
        Accounts in account_types that are no longer in the graph
        (e.g. after clean_edges) must be removed.
        """
        # Pre-populate with a stale account
        main_module.account_types["stale_acc"] = "personal"
        main_module.account_types["active_acc"] = "personal"

        g = MagicMock()
        g.get_accounts.return_value = ["active_acc"]   # stale_acc is gone
        g.clean_edges.return_value = None
        g.check_bipartite_subgraph.return_value = False

        ml = MagicMock()
        ml.predict_all.return_value = {"active_acc": (False, 0.1)}
        ml.get_communities.return_value = []

        with (
            patch("main.level_2_get_graph", return_value=g),
            patch("main.level_2_check_account", return_value=(True, "")),
            patch("main.MLLevel", return_value=ml),
        ):
            client.post("/check_accounts")

        assert "stale_acc" not in main_module.account_types
        assert "active_acc" in main_module.account_types


# ============================================================
# 11. POST /check_accounts — internal exception handling
# ============================================================

class TestCheckAccountsException:

    def test_exception_returns_internal_error_status(self):
        with patch("main.level_2_get_graph", side_effect=RuntimeError("db down")):
            resp = client.post("/check_accounts")
        body = resp.json()
        assert body["status"] == "INTERNAL_ERROR"
        assert "db down" in body["reason"]

    def test_exception_response_has_empty_level_lists(self):
        with patch("main.level_2_get_graph", side_effect=RuntimeError("db down")):
            resp = client.post("/check_accounts")
        body = resp.json()
        assert body["level_2"] == []
        assert body["level_3"] == []

    def test_exception_does_not_propagate_as_500(self):
        with patch("main.level_2_get_graph", side_effect=RuntimeError("db down")):
            resp = client.post("/check_accounts")
        assert resp.status_code == 200


# ============================================================
# 12. POST /check_transaction — response schema validation
# ============================================================

class TestResponseSchema:

    def test_check_transaction_always_has_status_and_reason(self):
        for ok, reason in [(True, ""), (False, "blocked")]:
            with (
                patch("main.level_1_check_transaction", return_value=(ok, reason)),
                patch("main.level_2_add_transaction", return_value=(True, "")),
            ):
                resp = client.post("/check_transaction", json=_tx_payload())
            body = resp.json()
            assert "status" in body
            assert "reason" in body

    def test_check_accounts_always_has_all_keys(self):
        g = MagicMock()
        g.get_accounts.return_value = []
        g.clean_edges.return_value = None

        with patch("main.level_2_get_graph", return_value=g):
            resp = client.post("/check_accounts")

        body = resp.json()
        for key in ("status", "reason", "level_2", "level_3"):
            assert key in body, f"Missing key: {key}"