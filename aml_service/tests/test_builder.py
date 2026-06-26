"""
Unit tests for graph/builder.py (TransactionsGraph)

Run with:
    pytest test_builder.py -v

Each test creates a fresh TransactionsGraph() — no shared state between tests.
Timestamps are plain integers for readability (the code only compares them with < / >=).
"""

import pytest
from datetime import datetime
from collections import deque
from graph.builder import TransactionsGraph


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def make_graph(*edges):
    """
    Convenience factory.
    edges: list of (from, to, amount, timestamp)
    Returns a TransactionsGraph with those edges already added.
    """
    g = TransactionsGraph()
    for from_acc, to_acc, amount, ts in edges:
        g.add_transaction(from_acc, to_acc, amount, ts)
    return g


# ---------------------------------------------------------------------------
# 1.  add_transaction / basic graph structure
# ---------------------------------------------------------------------------

class TestAddTransaction:

    def test_single_edge_creates_two_nodes(self):
        g = TransactionsGraph()
        g.add_transaction("A", "B", 100, 1)
        assert "A" in g.get_accounts()
        assert "B" in g.get_accounts()

    def test_self_loop_is_ignored(self):
        g = TransactionsGraph()
        g.add_transaction("A", "A", 500, 1)
        # No nodes should have been created
        assert "A" not in g.get_accounts()

    def test_parallel_edges_between_same_pair(self):
        """MultiDiGraph must allow multiple edges from A→B."""
        g = make_graph(("A", "B", 100, 1), ("A", "B", 200, 2))
        # Both amounts should be reflected in output money
        assert g.get_output_money("A") == 300

    def test_node_count(self):
        g = make_graph(("A", "B", 1, 1), ("B", "C", 1, 2), ("C", "D", 1, 3))
        assert g.get_nodes() == 4


# ---------------------------------------------------------------------------
# 2.  clean_edges
# ---------------------------------------------------------------------------

class TestCleanEdges:

    def test_old_edges_are_removed(self):
        g = make_graph(("A", "B", 100, 1), ("A", "B", 200, 5))
        g.clean_edges(cutoff_time=4)   # keep timestamp >= 4
        # Only the edge with timestamp=5 should remain
        assert g.get_output_money("A") == 200

    def test_isolated_nodes_removed_after_clean(self):
        g = make_graph(("A", "B", 100, 1))
        g.clean_edges(cutoff_time=2)
        assert "A" not in g.get_accounts()
        assert "B" not in g.get_accounts()

    def test_recent_edges_survive_clean(self):
        g = make_graph(("A", "B", 100, 10), ("A", "C", 50, 20))
        g.clean_edges(cutoff_time=5)
        assert g.get_output_money("A") == 150

    def test_empty_graph_clean_does_not_raise(self):
        g = TransactionsGraph()
        g.clean_edges(cutoff_time=100)   # should not raise


# ---------------------------------------------------------------------------
# 3.  get_indegree / get_outdegree
# ---------------------------------------------------------------------------

class TestDegrees:

    def test_outdegree_counts_distinct_successors(self):
        # Two edges to same successor → outdegree 1, not 2
        g = make_graph(("A", "B", 100, 1), ("A", "B", 200, 2), ("A", "C", 50, 3))
        assert g.get_outdegree("A") == 2   # B and C

    def test_indegree_counts_distinct_predecessors(self):
        g = make_graph(("A", "C", 100, 1), ("B", "C", 200, 2), ("B", "C", 50, 3))
        assert g.get_indegree("C") == 2    # A and B

    def test_degree_unknown_node_returns_zero(self):
        g = TransactionsGraph()
        assert g.get_outdegree("nobody") == 0
        assert g.get_indegree("nobody") == 0


# ---------------------------------------------------------------------------
# 4.  get_input_money / get_output_money
# ---------------------------------------------------------------------------

class TestMoneyTotals:

    def test_output_money_sums_all_outgoing_edges(self):
        g = make_graph(("A", "B", 300, 1), ("A", "C", 700, 2))
        assert g.get_output_money("A") == 1000

    def test_input_money_sums_all_incoming_edges(self):
        g = make_graph(("X", "Z", 400, 1), ("Y", "Z", 600, 2))
        assert g.get_input_money("Z") == 1000

    def test_money_unknown_node_returns_zero(self):
        g = TransactionsGraph()
        assert g.get_output_money("ghost") == 0
        assert g.get_input_money("ghost") == 0


# ---------------------------------------------------------------------------
# 5.  fast_get_money_cycled  (the DFS taint-tracker)
# ---------------------------------------------------------------------------

class TestFastGetMoneyCycled:

    def test_simple_cycle_full_amount(self):
        """A→B→A with 100; A should detect 100 cycled."""
        g = make_graph(("A", "B", 100, 1), ("B", "A", 100, 2))
        assert g.fast_get_money_cycled("A") == 100

    def test_no_cycle_returns_zero(self):
        g = make_graph(("A", "B", 100, 1), ("B", "C", 100, 2))
        assert g.fast_get_money_cycled("A") == 0

    def test_unknown_account_returns_zero(self):
        g = TransactionsGraph()
        assert g.fast_get_money_cycled("nobody") == 0

    def test_three_hop_cycle(self):
        """A→B→C→A, amount 500 each; A should detect 500 cycled."""
        g = make_graph(
            ("A", "B", 500, 1),
            ("B", "C", 500, 2),
            ("C", "A", 500, 3),
        )
        assert g.fast_get_money_cycled("A") == 500

    def test_temporal_order_enforced_backward_time_returns_zero(self):
        """B→A timestamp is BEFORE A→B — the cycle is temporally invalid."""
        g = make_graph(
            ("A", "B", 100, 10),
            ("B", "A", 100, 5),   # earlier than the A→B edge
        )
        assert g.fast_get_money_cycled("A") == 0

    def test_capacity_constrained_by_bottleneck(self):
        """A→B 1000, B→A 300.  Only 300 can cycle back."""
        g = make_graph(("A", "B", 1000, 1), ("B", "A", 300, 2))
        assert g.fast_get_money_cycled("A") == 300

    def test_parallel_outgoing_edges_tracked_independently(self):
        """
        A→B (100, t=1), A→B (200, t=2), B→A (150, t=3)
        Total outgoing to B = 300, but B only sends 150 back.
        """
        g = make_graph(
            ("A", "B", 100, 1),
            ("A", "B", 200, 2),
            ("B", "A", 150, 3),
        )
        assert g.fast_get_money_cycled("A") == 150

    def test_cycle_beyond_cutoff_not_detected(self):
        """
        LOOP_CUTOFF = 4 hops.  A 5-hop cycle must NOT be detected.
        A→B→C→D→E→A (5 edges = 5 hops; cutoff is 4)
        """
        g = make_graph(
            ("A", "B", 100, 1),
            ("B", "C", 100, 2),
            ("C", "D", 100, 3),
            ("D", "E", 100, 4),
            ("E", "A", 100, 5),
        )
        assert g.fast_get_money_cycled("A") == 0

    def test_cycle_within_cutoff_detected(self):
        """4-hop cycle: A→B→C→D→A (exactly at LOOP_CUTOFF)."""
        g = make_graph(
            ("A", "B", 100, 1),
            ("B", "C", 100, 2),
            ("C", "D", 100, 3),
            ("D", "A", 100, 4),
        )
        assert g.fast_get_money_cycled("A") == 100

    def test_two_independent_cycles_both_counted(self):
        """
        A→B→A (100) and A→C→A (200); total cycled = 300.
        """
        g = make_graph(
            ("A", "B", 100, 1),
            ("B", "A", 100, 2),
            ("A", "C", 200, 3),
            ("C", "A", 200, 4),
        )
        assert g.fast_get_money_cycled("A") == 300


# ---------------------------------------------------------------------------
# 6.  check_scatter_gather
# ---------------------------------------------------------------------------

class TestCheckScatterGather:

    def test_no_successors_returns_empty(self):
        g = TransactionsGraph()
        g.add_transaction("X", "Y", 1, 1)
        # "Y" has no successors
        accounts, val = g.check_scatter_gather("Y", levels=2)
        assert val == 0
        assert accounts == []

    def test_unknown_account_returns_empty(self):
        g = TransactionsGraph()
        accounts, val = g.check_scatter_gather("ghost", levels=2)
        assert val == 0
        assert accounts == []

    def test_scatter_below_threshold_terminates_early(self):
        """
        A fans out to only 2 accounts (B, C); threshold_scatter=3.
        Should return early with max_value = 1 and those accounts.
        """
        g = make_graph(("A", "B", 10, 1), ("A", "C", 20, 2))
        accounts, val = g.check_scatter_gather("A", levels=4, threshold_scatter=3)
        assert val == 1
        assert set(accounts) == {"B", "C"}

    def test_scatter_gather_two_levels(self):
        """
        Classic scatter-gather:
        A → B, C, D  (scatter, 3 intermediaries)
        B, C, D → E  (gather at E)
        Expected: E with gather value 3.
        """
        g = make_graph(
            ("A", "B", 10, 1), ("A", "C", 10, 1), ("A", "D", 10, 1),
            ("B", "E", 10, 2), ("C", "E", 10, 2), ("D", "E", 10, 2),
        )
        accounts, val = g.check_scatter_gather("A", levels=2, threshold_scatter=3)
        assert val == 3
        assert "E" in accounts

    def test_temporal_ordering_gather_before_scatter_excluded(self):
        """
        D→E has timestamp 0, which is before D received money from A (timestamp=1).
        That edge should be skipped.
        """
        g = make_graph(
            ("A", "B", 10, 1), ("A", "C", 10, 1), ("A", "D", 10, 1),
            ("B", "E", 10, 2), ("C", "E", 10, 2),
            ("D", "E", 10, 0),   # timestamp before D received from A
        )
        accounts, val = g.check_scatter_gather("A", levels=2, threshold_scatter=3)
        # D's contribution to E must be excluded; only B and C contribute → val=2
        assert val == 2

    def test_multiple_paths_to_same_gatherer_accumulate(self):
        """
        A → B, C, D (3 scatter)
        B → E, C → E, D → F
        E is reachable via 2 paths from 3-scatter level so val at E should be 2.
        """
        g = make_graph(
            ("A", "B", 1, 1), ("A", "C", 1, 1), ("A", "D", 1, 1),
            ("B", "E", 1, 2), ("C", "E", 1, 2), ("D", "F", 1, 2),
        )
        accounts, val = g.check_scatter_gather("A", levels=2, threshold_scatter=3)
        assert val == 2
        assert "E" in accounts


# ---------------------------------------------------------------------------
# 7.  check_bipartite_subgraph
# ---------------------------------------------------------------------------

class TestCheckBipartiteSubgraph:

    def test_known_bipartite_graph(self):
        """
        A→B, A→D, C→B, C→D — classic 2-partition bipartite {A,C} and {B,D}.
        """
        g = make_graph(
            ("A", "B", 1, 1), ("A", "D", 1, 1),
            ("C", "B", 1, 1), ("C", "D", 1, 1),
        )
        # community must have at least minimum_graph_size=4 nodes
        assert g.check_bipartite_subgraph(["A", "B", "C", "D"], minimum_graph_size=4) is True

    def test_triangle_is_not_bipartite(self):
        """A→B→C→A is an odd cycle — not bipartite."""
        g = make_graph(("A", "B", 1, 1), ("B", "C", 1, 2), ("C", "A", 1, 3))
        assert g.check_bipartite_subgraph(["A", "B", "C"], minimum_graph_size=3) is False

    def test_below_minimum_graph_size_returns_false(self):
        """Community smaller than minimum_graph_size → immediately False."""
        g = make_graph(("A", "B", 1, 1), ("C", "D", 1, 1))
        # Only 2 accounts in community, minimum_graph_size=5
        assert g.check_bipartite_subgraph(["A", "B"], minimum_graph_size=5) is False

    def test_community_members_not_in_graph_excluded(self):
        """Accounts in community list but absent from graph are silently ignored."""
        g = make_graph(
            ("A", "B", 1, 1), ("A", "D", 1, 1),
            ("C", "B", 1, 1), ("C", "D", 1, 1),
        )
        # "GHOST1" and "GHOST2" are not in the graph — should not crash
        result = g.check_bipartite_subgraph(
            ["A", "B", "C", "D", "GHOST1", "GHOST2"],
            minimum_graph_size=4
        )
        # A,B,C,D form a bipartite subgraph
        assert result is True

    def test_disconnected_bipartite_components(self):
        """Two separate bipartite components: {A,B} and {C,D}."""
        g = make_graph(("A", "B", 1, 1), ("C", "D", 1, 1))
        assert g.check_bipartite_subgraph(["A", "B", "C", "D"], minimum_graph_size=4) is True

    def test_single_edge_bipartite(self):
        """Single edge A→B is trivially bipartite when min_size=2."""
        g = make_graph(("A", "B", 1, 1))
        assert g.check_bipartite_subgraph(["A", "B"], minimum_graph_size=2) is True

    def test_five_cycle_is_not_bipartite(self):
        """A 5-cycle is an odd cycle — not bipartite."""
        g = make_graph(
            ("A", "B", 1, 1), ("B", "C", 1, 2),
            ("C", "D", 1, 3), ("D", "E", 1, 4),
            ("E", "A", 1, 5),
        )
        assert g.check_bipartite_subgraph(
            ["A", "B", "C", "D", "E"], minimum_graph_size=5
        ) is False

    def test_four_cycle_is_bipartite(self):
        """A 4-cycle (even) is bipartite: {A,C} and {B,D}."""
        g = make_graph(
            ("A", "B", 1, 1), ("B", "C", 1, 2),
            ("C", "D", 1, 3), ("D", "A", 1, 4),
        )
        assert g.check_bipartite_subgraph(
            ["A", "B", "C", "D"], minimum_graph_size=4
        ) is True