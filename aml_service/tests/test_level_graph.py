"""
Unit tests for levels/level_graph.py

Run with:
    pytest test_level_graph.py -v

Design notes
------------
* level_graph.py opens thresholds.json at import time and holds a module-level
  TransactionsGraph singleton called `graph`.  We patch both before importing.
* Each test replaces `lr_g.graph` with a fresh TransactionsGraph() so tests
  never bleed state into each other.
* Timestamps are plain integers (the graph only uses < / >= comparisons).
"""

import json
import pytest
from unittest.mock import patch, mock_open
from datetime import datetime

# ---------------------------------------------------------------------------
# Thresholds that satisfy both check_transaction and check_account paths
# ---------------------------------------------------------------------------

MOCK_THRESHOLDS = {
    "personal": {
        "tx_threshold": 10_000,
        "daily_limit": 50_000,
        "max_fan_out": 3,
        "max_fan_in": 3,
        "output_money_amount_check_cycles": 1_000,
        "cycle_amount_percentage": 0.5,   # cycled_threshold = 500
        "scatter_accounts_threshold": 3,
        "gather_accounts_threshold": 3,
        "gather_scatter_money_ratio": 2.0,
    },
}

with patch(
    "builtins.open",
    mock_open(read_data=json.dumps(MOCK_THRESHOLDS)),
):
    import levels.level_graph as lr_g
    from graph.builder import TransactionsGraph


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(autouse=True)
def fresh_graph():
    """Replace the module-level singleton with a clean graph for every test."""
    lr_g.graph = TransactionsGraph()
    yield
    lr_g.graph = TransactionsGraph()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _tx(
    sender="alice",
    receiver="bob",
    amount=100,
    account_type="personal",
    receiver_account_type="personal",
    timestamp=None,
):
    return {
        "sender": sender,
        "receiver": receiver,
        "amount": amount,
        "sender_account_type": account_type,
        "receiver_account_type": receiver_account_type,
        "timestamp": timestamp or 1,
    }


# ---------------------------------------------------------------------------
# 1. check_transaction — happy path
# ---------------------------------------------------------------------------

class TestCheckTransactionValid:

    def test_valid_transaction_accepted(self):
        ok, msg = lr_g.check_transaction(_tx())
        assert ok is True
        assert msg == ""

    def test_transaction_added_to_graph(self):
        lr_g.check_transaction(_tx(sender="A", receiver="B", amount=200))
        assert lr_g.graph.get_output_money("A") == 200

    def test_unknown_account_type_rejected(self):
        ok, msg = lr_g.check_transaction(_tx(account_type="government"))
        assert ok is False
        assert "Wrong Account type" in msg


# ---------------------------------------------------------------------------
# 2. check_transaction — fan-out (sender sends to too many distinct accounts)
# ---------------------------------------------------------------------------

class TestSenderFanOut:
    """max_fan_out = 3 for 'personal'."""

    def test_fan_out_exactly_at_limit_accepted(self):
        for dest in ["B", "C", "D"]:
            ok, _ = lr_g.check_transaction(_tx(sender="A", receiver=dest))
        assert ok is True

    def test_fan_out_exceeds_limit_rejected(self):
        for dest in ["B", "C", "D"]:
            lr_g.check_transaction(_tx(sender="A", receiver=dest))
        ok, msg = lr_g.check_transaction(_tx(sender="A", receiver="E"))
        assert ok is False
        assert "fan out" in msg.lower()

    def test_multiple_edges_to_same_receiver_not_counted_twice(self):
        """Two transactions A→B count as fan_out=1, not 2."""
        lr_g.check_transaction(_tx(sender="A", receiver="B", amount=50))
        ok, _ = lr_g.check_transaction(_tx(sender="A", receiver="B", amount=50))
        assert ok is True


# ---------------------------------------------------------------------------
# 3. check_transaction — receiver fan-in
# ---------------------------------------------------------------------------

class TestReceiverFanIn:
    """max_fan_in = 3 for 'personal'."""

    def test_fan_in_exactly_at_limit_accepted(self):
        for src in ["X", "Y", "Z"]:
            ok, _ = lr_g.check_transaction(_tx(sender=src, receiver="E"))
        assert ok is True

    def test_fan_in_exceeds_limit_rejected(self):
        for src in ["X", "Y", "Z"]:
            lr_g.check_transaction(_tx(sender=src, receiver="E"))
        ok, msg = lr_g.check_transaction(_tx(sender="W", receiver="E"))
        assert ok is False
        assert "fan in" in msg.lower()

    def test_no_receiver_account_type_skips_fan_in_check(self):
        """If receiver_account_type is absent, fan-in check is skipped."""
        tx = {
            "sender": "A",
            "receiver": "E",
            "amount": 100,
            "sender_account_type": "personal",
            "timestamp": 1,
        }
        for src in ["X", "Y", "Z", "W"]:  # 4 senders
            tx["sender"] = src
            ok, _ = lr_g.check_transaction(tx)
        # Should pass because receiver limits were never checked
        assert ok is True


# ---------------------------------------------------------------------------
# 4. check_transaction — money cycling detection (sender side)
# ---------------------------------------------------------------------------

class TestSenderCyclingDetection:
    """
    output_money_amount_check_cycles = 1_000
    cycle_amount_percentage = 0.5  →  cycled_threshold = 500
    Cycle check only runs when get_output_money >= 1_000.
    """

    def test_cycling_below_output_threshold_not_checked(self):
        """Output < 1_000 → cycle check skipped → accepted."""
        lr_g.graph.add_transaction("A", "B", 900, 1)
        lr_g.graph.add_transaction("B", "A", 900, 2)
        ok, _ = lr_g.check_transaction(_tx(sender="A", receiver="B", amount=50, timestamp=3))
        assert ok is True

    def test_cycling_above_output_threshold_but_below_cycled_threshold_accepted(self):
        """Output >= 1_000, but cycled < 500 → accepted."""
        lr_g.graph.add_transaction("A", "B", 1_000, 1)
        lr_g.graph.add_transaction("B", "A", 400, 2)   # 400 < 500 threshold
        ok, _ = lr_g.check_transaction(_tx(sender="A", receiver="C", amount=1, timestamp=3))
        assert ok is True

    def test_cycling_flagged_when_above_threshold(self):
        """Output >= 1_000, cycled >= 500 → rejected."""
        # Pre-seed a cycle: A→B 1000 (t=1), B→A 600 (t=2)
        lr_g.graph.add_transaction("A", "B", 1_000, 1)
        lr_g.graph.add_transaction("B", "A", 600, 2)
        # Now trigger check_transaction which calls fast_get_money_cycled
        ok, msg = lr_g.check_transaction(_tx(sender="A", receiver="C", amount=1, timestamp=3))
        assert ok is False
        assert "cycled" in msg.lower()


# ---------------------------------------------------------------------------
# 5. add_transaction  (the audit helper, no AML checks)
# ---------------------------------------------------------------------------

class TestAddTransaction:

    def test_valid_add_transaction(self):
        tx = {
            "sender": "A", "receiver": "B", "amount": 500,
            "sender_account_type": "personal", "timestamp": 1,
        }
        ok, msg = lr_g.add_transaction(tx)
        assert ok is True
        assert msg == ""

    def test_add_transaction_builds_graph(self):
        tx = {
            "sender": "A", "receiver": "B", "amount": 300,
            "sender_account_type": "personal", "timestamp": 1,
        }
        lr_g.add_transaction(tx)
        assert lr_g.graph.get_output_money("A") == 300

    def test_add_transaction_wrong_account_type(self):
        tx = {
            "sender": "A", "receiver": "B", "amount": 100,
            "sender_account_type": "unknown", "timestamp": 1,
        }
        ok, msg = lr_g.add_transaction(tx)
        assert ok is False
        assert "Wrong Account type" in msg

    def test_add_transaction_missing_key_returns_error(self):
        ok, msg = lr_g.add_transaction({"sender": "A"})
        assert ok is False
        assert "error" in msg.lower()


# ---------------------------------------------------------------------------
# 6. check_account — fan-out / fan-in limits
# ---------------------------------------------------------------------------

class TestCheckAccountFanLimits:

    def test_account_within_fan_out_limit_accepted(self):
        for dest in ["B", "C"]:
            lr_g.graph.add_transaction("A", dest, 100, 1)
        ok, _ = lr_g.check_account("A", "personal")
        assert ok is True

    def test_account_exceeds_fan_out_rejected(self):
        for dest in ["B", "C", "D", "E"]:   # 4 > max_fan_out=3
            lr_g.graph.add_transaction("A", dest, 100, 1)
        ok, msg = lr_g.check_account("A", "personal")
        assert ok is False
        assert "fan out" in msg.lower()

    def test_account_exceeds_fan_in_rejected(self):
        for src in ["X", "Y", "Z", "W"]:   # 4 > max_fan_in=3
            lr_g.graph.add_transaction(src, "E", 100, 1)
        ok, msg = lr_g.check_account("E", "personal")
        assert ok is False
        assert "fan in" in msg.lower()


# ---------------------------------------------------------------------------
# 7. check_account — gather-scatter (combined fan-in AND fan-out)
# ---------------------------------------------------------------------------

class TestCheckAccountGatherScatter:
    """
    Gather-scatter fires when BOTH fan_out > max_fan_out AND fan_in > max_fan_in
    AND money_out / money_in > gather_scatter_money_ratio (2.0).
    """

    def test_gather_scatter_money_ratio_exceeded(self):
        # E receives from 4 sources (fan_in=4 > 3) and sends to 4 targets (fan_out=4 > 3)
        for src in ["A", "B", "C", "D"]:
            lr_g.graph.add_transaction(src, "E", 100, 1)   # total in = 400
        for dest in ["F", "G", "H", "I"]:
            lr_g.graph.add_transaction("E", dest, 300, 2)  # total out = 1200; ratio 3 > 2
        ok, msg = lr_g.check_account("E", "personal")
        assert ok is False
        assert "gather" in msg.lower() or "scatter" in msg.lower()

    def test_gather_scatter_low_ratio_not_flagged_by_ratio_check(self):
        """Fan-in and fan-out both exceeded, but ratio is fine.
           The individual fan checks will still fire — but the ratio branch won't."""
        for src in ["A", "B", "C", "D"]:
            lr_g.graph.add_transaction(src, "E", 100, 1)  # in=400
        for dest in ["F", "G", "H", "I"]:
            lr_g.graph.add_transaction("E", dest, 100, 2)  # out=400; ratio=1 < 2
        ok, msg = lr_g.check_account("E", "personal")
        # The individual fan checks will flag this — but NOT the ratio branch
        assert ok is False
        assert "ratio" not in msg.lower()


# ---------------------------------------------------------------------------
# 8. check_account — scatter-gather pattern  (BFS detection)
# ---------------------------------------------------------------------------

class TestCheckAccountScatterGather:
    """
    scatter_accounts_threshold = 3
    gather_accounts_threshold  = 3
    A scatter-gather is flagged when max convergence value >= 3.
    """

    def test_scatter_gather_pattern_detected(self):
        """
        A → B, C, D  (3-way scatter)
        B, C, D → E  (full gather at E)
        """
        for dest in ["B", "C", "D"]:
            lr_g.graph.add_transaction("A", dest, 100, 1)
        for src in ["B", "C", "D"]:
            lr_g.graph.add_transaction(src, "E", 100, 2)
        ok, msg = lr_g.check_account("A", "personal")
        assert ok is False
        assert "scatter" in msg.lower() or "gather" in msg.lower()

    def test_scatter_gather_below_threshold_not_detected(self):
        """
        A → B, C  (2-way scatter, below threshold of 3)
        B, C → E
        """
        lr_g.graph.add_transaction("A", "B", 100, 1)
        lr_g.graph.add_transaction("A", "C", 100, 1)
        lr_g.graph.add_transaction("B", "E", 100, 2)
        lr_g.graph.add_transaction("C", "E", 100, 2)
        ok, _ = lr_g.check_account("A", "personal")
        assert ok is True

    def test_unknown_account_type_rejected(self):
        ok, msg = lr_g.check_account("A", "unknown")
        assert ok is False
        assert "Wrong Account type" in msg


# ---------------------------------------------------------------------------
# 9. check_account — money cycling
# ---------------------------------------------------------------------------

class TestCheckAccountCycling:

    def test_cycling_flagged(self):
        """A→B 1000 (t=1), B→A 600 (t=2) → cycled=600 >= threshold 500."""
        lr_g.graph.add_transaction("A", "B", 1_000, 1)
        lr_g.graph.add_transaction("B", "A", 600, 2)
        ok, msg = lr_g.check_account("A", "personal")
        assert ok is False
        assert "cycled" in msg.lower()

    def test_cycling_below_threshold_accepted(self):
        """A→B 1000 (t=1), B→A 400 (t=2) → cycled=400 < threshold 500."""
        lr_g.graph.add_transaction("A", "B", 1_000, 1)
        lr_g.graph.add_transaction("B", "A", 400, 2)
        ok, _ = lr_g.check_account("A", "personal")
        assert ok is True

    def test_cycling_check_skipped_below_output_threshold(self):
        """Output < 1_000 → cycling check never runs."""
        lr_g.graph.add_transaction("A", "B", 900, 1)
        lr_g.graph.add_transaction("B", "A", 900, 2)
        ok, _ = lr_g.check_account("A", "personal")
        assert ok is True