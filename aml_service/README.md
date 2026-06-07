# AML Transaction Monitoring Engine

## Architecture Overview

The engine evaluates transactions through Three distinct layers:

1. **Level 1: Heuristic Rules Engine (`level_rules.py`)** Monitors high-speed, high-volume, and structuring (smurfing) behaviors using in-memory rolling time windows.
2. **Level 2: Graph Topology Engine (`level_graph.py` & `builder.py`)**
Monitors the shape of the financial network, flagging abnormal fan-in/fan-out behaviors and detecting complex, time-respecting circular money laundering loops.
3. **Level 3: Machine Learning (TO BE DONE)**

---

## 1. Heuristic Rules Engine (`level_rules.py`)

This module evaluates the raw volume and velocity of transactions against specific account types. It maintains an in-memory `daily_window` for senders to calculate running totals.

**Key Detection Capabilities:**

* **Transaction Limits:** Flags single transactions exceeding the absolute maximum threshold.
* **Daily Limits:** Flags transactions that push a sender's 24-hour rolling total over the daily limit.
* **Structuring (Smurfing):** Detects when a user attempts to bypass reporting thresholds by breaking down large sums. It triggers if a user makes multiple transactions that are just below the threshold (e.g., 90% of the limit) within a 24-hour window.
* **Velocity / Rapid Transactions:** Flags bursts of rapid-fire activity (e.g., executing 5 transactions within a 1-minute window).

---

## 2. Graph Topology Engine (`builder.py` & `level_graph.py`)

This module utilizes `NetworkX` (`MultiDiGraph`) to model financial relationships as a directed graph. Nodes are accounts, and edges are individual timestamped transactions.

### Topology Checks (Fan-In / Fan-Out)

Instead of counting raw transaction volume, the engine measures unique counterparties using `successors` and `predecessors`.

* **High Fan-Out (Layering):** Flags senders dispersing funds to an abnormally high number of distinct accounts.
* **High Fan-In (Mule Collection):** Flags receivers consolidating funds from an abnormally high number of distinct accounts.

### Time-Respecting Cycle Detection

The most complex feature of this engine is `get_money_cycled`, which detects when a user outputs funds that eventually route back to them through intermediaries.

**Mathematical Design Decisions for Peers:**
Calculating Maximum Flow on a chronological, multi-edge graph is computationally expensive. To make this production-ready and prevent server lockups, this algorithm uses a **time-respecting greedy approximation**:

1. **Shortest-Path First:** The engine sorts paths by length (`key=len`) using a maximum `LOOP_CUTOFF` of 4 hops. This prevents long, convoluted paths from accidentally "stealing" network capacity from shorter, more direct laundering loops.
2. **Residual Capacity Tracking:** To prevent the "Combinatorial Double-Counting" trap (where multiple overlapping paths reuse the same historical transaction), the engine maintains a `remaining_capacity` dictionary. Once a path consumes funds from an edge, that edge's capacity is reduced.
3. **Strict Chronology:** The algorithm verifies that every hop in a path occurred *after* the previous hop. "Time travel" paths are instantly discarded.
4. **Guaranteed Lower-Bound:** Because it relies on a greedy shortest-path heuristic rather than a full Time-Expanded Network structure, this algorithm provides a mathematically sound *lower bound*. If it reports $5,000 cycled, it is guaranteed that at least $5,000 cycled, resulting in highly defensible alerts with near-zero mathematical false positives.

Both the sender and receiver are checked during a transaction to see if the current transfer completes a cycle based on their historical output volume.

---

## Configuration (`thresholds.json`)

All monitoring thresholds are decoupled from the logic and stored in `config/thresholds.json`. Thresholds are segmented by `account_type` (e.g., individual vs. business).

**Key Parameters:**

* `tx_threshold`: Maximum allowed per-transaction amount.
* `structuring_percentage`: The threshold ratio to trigger smurfing checks (e.g., `0.9` means checking for amounts >= 90% of `tx_threshold`).
* `velocity_window_minutes`: The time window to evaluate rapid transactions.
* `max_fan_in` / `max_fan_out`: Maximum allowed unique counterparties.
* `output_money_amount_check_cycles`: The minimum historical output volume required before the engine spends CPU cycles searching for graph loops.
* `cycle_amount_percentage`: The percentage of outputted money that must return to the user to trigger a laundering alert (e.g., `0.7` = 70% cycled back).

---

## How to Run

### Prerequisites

Ensure you have Python 3 installed along with the required dependencies:

```bash
pip install networkx

```

### Usage

The engine is designed to be imported and used as a validation step in your transaction processing pipeline.

```python
from levels.level_rules import check_transaction as check_rules
from levels.level_graph import check_transaction as check_graph

# Sample incoming transaction
tx = {
    "account_type": "individual",
    "amount": 5000,
    "sender": "Account_A",
    "receiver": "Account_B",
    "timestamp": datetime.now()
}

# 1. Run basic heuristic rules
is_valid_rules, rules_reason = check_rules(tx)
if not is_valid_rules:
    print(f"Alert Triggered (Rules): {rules_reason}")

# 2. Run graph topology and cycle checks
is_valid_graph, graph_reason = check_graph(tx)
if not is_valid_graph:
    print(f"Alert Triggered (Graph): {graph_reason}")

```

## Next Steps
**for level 2 implement your own dfs to handle timestamps pruning instead of checking after dfs**
**reference for money cycles: https://arxiv.org/pdf/2011.09318**
**for level start level 3 ML based on those papers**
**https://arxiv.org/pdf/2506.04292v3**
**https://arxiv.org/pdf/2112.07508**
**https://arxiv.org/pdf/2405.19383v1**

