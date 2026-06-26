"""
Unit tests for levels/level_rules.py

Run with:
    pytest test_level_rules.py -v

The module uses a global `daily_transactions` defaultdict that persists across
calls, so every test must start with a clean slate.  The `reset_daily` fixture
handles that automatically.
"""

import json
import os
import sys
import pytest
from datetime import datetime, timedelta
from unittest.mock import patch, mock_open


# ---------------------------------------------------------------------------
# Minimal thresholds fixture so the module can be imported without a real file
# ---------------------------------------------------------------------------

MOCK_THRESHOLDS = {
    "personal": {
        "tx_threshold": 10_000,
        "daily_limit": 50_000,
        "structuring_percentage": 0.8,   # structuring_threshold = 8_000
        "structuring_limit": 3,          # 3+ transactions >= 8_000 → flagged
        "velocity_window_minutes": 10,
        "velocity_limit": 3,             # 3+ transactions within 10 min → flagged
    },
    "business": {
        "tx_threshold": 100_000,
        "daily_limit": 500_000,
        "structuring_percentage": 0.8,
        "structuring_limit": 5,
        "velocity_window_minutes": 5,
        "velocity_limit": 5,
    },
}

# Patch the open() call that happens at module import time so we don't need the
# real thresholds.json on disk.
with patch(
    "builtins.open",
    mock_open(read_data=json.dumps(MOCK_THRESHOLDS)),
):
    import levels.level_rules as lr


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _tx(
    sender="alice",
    amount=1_000,
    account_type="personal",
    timestamp=None,
):
    """Build a minimal transaction dict."""
    return {
        "sender": sender,
        "amount": amount,
        "sender_account_type": account_type,
        "timestamp": timestamp or datetime.now(),
    }


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(autouse=True)
def reset_daily():
    """Clear the module-level daily_transactions before (and after) every test.

    Using autouse=True means every test in this file gets a clean slate without
    having to request the fixture explicitly.
    """
    lr.daily_transactions.clear()
    yield
    lr.daily_transactions.clear()


# ---------------------------------------------------------------------------
# 1. Happy-path: valid transaction
# ---------------------------------------------------------------------------

class TestValidTransaction:

    def test_valid_transaction_is_accepted(self):
        ok, msg = lr.check_transaction(_tx(amount=500))
        assert ok is True
        assert msg == ""

    def test_accepted_transaction_is_appended_to_daily_log(self):
        ts = datetime(2025, 1, 1, 12, 0, 0)
        lr.check_transaction(_tx(sender="bob", amount=200, timestamp=ts))
        assert len(lr.daily_transactions["bob"]) == 1
        assert lr.daily_transactions["bob"][0][0] == 200

    def test_multiple_valid_transactions_accumulate(self):
        for _ in range(3):
            ok, _ = lr.check_transaction(_tx(amount=100))
        assert ok is True
        assert len(lr.daily_transactions["alice"]) == 3


# ---------------------------------------------------------------------------
# 2. tx_threshold enforcement
# ---------------------------------------------------------------------------

class TestTxThreshold:

    def test_amount_exactly_at_threshold_is_rejected(self):
        """Boundary: amount == tx_threshold should be rejected (> check)."""
        # tx_threshold for "personal" is 10_000; amount 10_001 must fail
        ok, msg = lr.check_transaction(_tx(amount=10_001))
        assert ok is False
        assert "exceeds transaction limit" in msg

    def test_amount_equal_to_threshold_passes(self):
        """amount == tx_threshold: the check is strict '>' so this passes."""
        ok, msg = lr.check_transaction(_tx(amount=10_000))
        assert ok is True

    def test_amount_clearly_over_threshold_is_rejected(self):
        ok, msg = lr.check_transaction(_tx(amount=99_999))
        assert ok is False
        assert "exceeds transaction limit" in msg


# ---------------------------------------------------------------------------
# 3. Daily limit enforcement
# ---------------------------------------------------------------------------

class TestDailyLimit:

    def test_daily_limit_exact_boundary_rejected(self):
        """Pre-fill daily log so that the new tx would hit exactly daily_limit."""
        ts_base = datetime(2025, 6, 1, 10, 0, 0)
        # 4 previous transactions × 9_000 = 36_000; new 14_001 would hit 50_001
        for i in range(4):
            lr.daily_transactions["alice"].append(
                [9_000, ts_base + timedelta(minutes=i)]
            )
        ok, msg = lr.check_transaction(
            _tx(amount=14_001, timestamp=ts_base + timedelta(minutes=10))
        )
        assert ok is False
        assert "Daily" in msg

    def test_daily_limit_just_under_passes(self):
        ts_base = datetime(2025, 6, 1, 10, 0, 0)
        for i in range(4):
            lr.daily_transactions["alice"].append(
                [9_000, ts_base + timedelta(minutes=i)]
            )
        # 36_000 + 13_999 = 49_999 < 50_000 → should pass
        ok, _ = lr.check_transaction(
            _tx(amount=13_999, timestamp=ts_base + timedelta(minutes=10))
        )
        assert ok is True

    def test_old_transactions_outside_24h_window_are_not_counted(self):
        """Transactions older than 24 h must be pruned by clean_daily."""
        now = datetime(2025, 6, 2, 12, 0, 0)
        old = now - timedelta(hours=25)
        # Add an old entry that would push us over the daily limit
        lr.daily_transactions["alice"].append([49_000, old])
        # A fresh 9_000 transaction should still be accepted
        ok, _ = lr.check_transaction(_tx(amount=9_000, timestamp=now))
        assert ok is True


# ---------------------------------------------------------------------------
# 4. Structuring detection
# ---------------------------------------------------------------------------

class TestStructuringDetection:
    """structuring_threshold = tx_threshold * structuring_percentage = 8_000
       structuring_limit = 3  → 3rd transaction >= 8_000 is flagged.
    """

    STRUCTURING_AMOUNT = 8_500   # >= 8_000

    def _prefill(self, n, ts_base):
        for i in range(n):
            lr.daily_transactions["alice"].append(
                [self.STRUCTURING_AMOUNT, ts_base + timedelta(hours=i)]
            )

    def test_at_structuring_limit_is_rejected(self):
        ts = datetime(2025, 6, 1, 8, 0, 0)
        # 2 already in log; new one would be the 3rd → should be flagged
        self._prefill(2, ts)
        ok, msg = lr.check_transaction(
            _tx(amount=self.STRUCTURING_AMOUNT, timestamp=ts + timedelta(hours=3))
        )
        assert ok is False
        assert "nearing limit" in msg

    def test_below_structuring_limit_passes(self):
        ts = datetime(2025, 6, 1, 8, 0, 0)
        # 1 already in log; new one is the 2nd → below limit of 3
        self._prefill(1, ts)
        ok, _ = lr.check_transaction(
            _tx(amount=self.STRUCTURING_AMOUNT, timestamp=ts + timedelta(hours=2))
        )
        assert ok is True

    def test_amount_below_structuring_threshold_not_counted(self):
        """Transactions below structuring_threshold must not be counted."""
        ts = datetime(2025, 6, 1, 8, 0, 0)
        # Fill log with amounts just below the structuring threshold
        for i in range(10):
            lr.daily_transactions["alice"].append([7_999, ts + timedelta(hours=i)])
        ok, _ = lr.check_transaction(
            _tx(amount=7_999, timestamp=ts + timedelta(hours=11))
        )
        assert ok is True


# ---------------------------------------------------------------------------
# 5. Velocity detection
# ---------------------------------------------------------------------------

class TestVelocityDetection:
    """velocity_limit = 3, velocity_window_minutes = 10
       3rd transaction within the same 10-minute window → flagged.
    """

    def test_at_velocity_limit_is_rejected(self):
        now = datetime(2025, 6, 1, 9, 0, 0)
        # 2 previous transactions within the last 10 minutes
        lr.daily_transactions["alice"].append([100, now - timedelta(minutes=3)])
        lr.daily_transactions["alice"].append([100, now - timedelta(minutes=6)])
        ok, msg = lr.check_transaction(_tx(amount=100, timestamp=now))
        assert ok is False
        assert "short time" in msg

    def test_below_velocity_limit_passes(self):
        now = datetime(2025, 6, 1, 9, 0, 0)
        # Only 1 previous transaction in the window
        lr.daily_transactions["alice"].append([100, now - timedelta(minutes=5)])
        ok, _ = lr.check_transaction(_tx(amount=100, timestamp=now))
        assert ok is True

    def test_old_velocity_transactions_outside_window_not_counted(self):
        """Transactions outside velocity_window_minutes must not trigger velocity."""
        now = datetime(2025, 6, 1, 9, 0, 0)
        # 2 transactions well outside the 10-minute window
        lr.daily_transactions["alice"].append([100, now - timedelta(minutes=15)])
        lr.daily_transactions["alice"].append([100, now - timedelta(minutes=20)])
        ok, _ = lr.check_transaction(_tx(amount=100, timestamp=now))
        assert ok is True


# ---------------------------------------------------------------------------
# 6. Unknown account type
# ---------------------------------------------------------------------------

class TestUnknownAccountType:

    def test_unknown_account_type_rejected(self):
        ok, msg = lr.check_transaction(
            _tx(account_type="government")  # not in THRESHOLDS
        )
        assert ok is False
        assert "account type" in msg.lower()

    def test_empty_string_account_type_rejected(self):
        ok, msg = lr.check_transaction(_tx(account_type=""))
        assert ok is False


# ---------------------------------------------------------------------------
# 7. clean_daily helper
# ---------------------------------------------------------------------------

class TestCleanDaily:

    def test_entries_older_than_24h_are_removed(self):
        now = datetime(2025, 6, 1, 12, 0, 0)
        old = now - timedelta(hours=25)
        lr.daily_transactions["carol"].append([500, old])
        lr.clean_daily("carol", now)
        assert len(lr.daily_transactions["carol"]) == 0

    def test_entries_within_24h_are_kept(self):
        now = datetime(2025, 6, 1, 12, 0, 0)
        recent = now - timedelta(hours=23)
        lr.daily_transactions["carol"].append([500, recent])
        lr.clean_daily("carol", now)
        assert len(lr.daily_transactions["carol"]) == 1

    def test_boundary_exactly_24h_ago_is_removed(self):
        """The cutoff is strict: timestamp > cutoff, so exactly 24 h is removed."""
        now = datetime(2025, 6, 1, 12, 0, 0)
        exactly_24h = now - timedelta(hours=24)
        lr.daily_transactions["carol"].append([500, exactly_24h])
        lr.clean_daily("carol", now)
        assert len(lr.daily_transactions["carol"]) == 0

    def test_unknown_sender_does_not_raise(self):
        """clean_daily on an unknown sender must not raise."""
        lr.clean_daily("nobody", datetime.now())  # should not raise


# ---------------------------------------------------------------------------
# 8. Exception / malformed input handling
# ---------------------------------------------------------------------------

class TestExceptionHandling:

    def test_missing_sender_key_returns_error(self):
        ok, msg = lr.check_transaction({"amount": 100, "sender_account_type": "personal"})
        assert ok is False
        assert "error" in msg.lower()

    def test_missing_amount_key_returns_error(self):
        ok, msg = lr.check_transaction({"sender": "alice", "sender_account_type": "personal"})
        assert ok is False
        assert "error" in msg.lower()

    def test_non_numeric_amount_returns_error(self):
        ok, msg = lr.check_transaction(
            _tx(amount="not_a_number")
        )
        assert ok is False
        assert "error" in msg.lower()

    def test_empty_dict_returns_error(self):
        ok, msg = lr.check_transaction({})
        assert ok is False


# ---------------------------------------------------------------------------
# 9. Business account type (spot-check higher limits)
# ---------------------------------------------------------------------------

class TestBusinessAccount:

    def test_business_large_valid_transaction(self):
        ok, _ = lr.check_transaction(_tx(amount=90_000, account_type="business"))
        assert ok is True

    def test_business_exceeds_tx_threshold(self):
        ok, msg = lr.check_transaction(_tx(amount=100_001, account_type="business"))
        assert ok is False
        assert "exceeds transaction limit" in msg