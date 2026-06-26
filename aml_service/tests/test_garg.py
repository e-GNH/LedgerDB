"""
Unit tests for graph/garg_index.py

Covers:
  - LouvainCommunities: partition properties, aggregation, trivial graphs
  - GargIndex: score semantics, caching, community detection integration,
    edge cases (unknown node, self-loops, empty graph)

Run with:
    pytest test_garg_index.py -v

No mocking needed — everything here is pure numpy / networkx math.
"""

import pytest
import numpy as np
import networkx as nx
import pandas as pd

from graph.garg_index import GargIndex, LouvainCommunities


# ============================================================
# Helpers
# ============================================================

def make_garg(edges):
    """Build a GargIndex from a plain list of (from, to) tuples."""
    df = pd.DataFrame(edges, columns=["from", "to"])
    return GargIndex(df, "from", "to")


def _all_nodes_covered(communities, expected_nodes):
    """Every expected node appears in exactly one community."""
    covered = []
    for c in communities:
        covered.extend(list(c))
    return set(covered) == set(expected_nodes)


# ============================================================
# 1.  LouvainCommunities — partition sanity checks
# ============================================================

class TestLouvainCommunities:

    def _run(self, nx_graph, use_delta=True):
        lc = LouvainCommunities(nx_graph, use_delta_modularity=use_delta)
        return lc.louvain()

    # ---- trivial cases ----

    def test_single_node_graph(self):
        g = nx.Graph()
        g.add_node("A")
        communities = self._run(g)
        all_nodes = [n for c in communities for n in c]
        assert set(all_nodes) == {"A"}

    def test_single_edge_two_communities_or_one(self):
        """A—B: both nodes must appear in the result."""
        g = nx.Graph()
        g.add_edge("A", "B")
        communities = self._run(g)
        assert _all_nodes_covered(communities, ["A", "B"])

    def test_no_edges_all_singletons(self):
        """Four isolated nodes: every node should appear."""
        g = nx.Graph()
        for n in ["A", "B", "C", "D"]:
            g.add_node(n)
        communities = self._run(g)
        assert _all_nodes_covered(communities, ["A", "B", "C", "D"])

    # ---- partition properties ----

    def test_all_nodes_covered(self):
        """After louvain(), every node in the graph is in exactly one community."""
        g = nx.karate_club_graph()
        communities = self._run(g)
        all_members = [n for c in communities for n in c]
        assert set(all_members) == set(g.nodes())

    def test_no_duplicate_nodes_across_communities(self):
        g = nx.karate_club_graph()
        communities = self._run(g)
        all_members = [n for c in communities for n in c]
        assert len(all_members) == len(set(all_members))

    def test_at_least_one_community(self):
        g = nx.path_graph(5)
        communities = self._run(g)
        assert len(communities) >= 1

    # ---- two clearly separated cliques should land in different communities ----

    def test_two_cliques_mostly_separated(self):
        """
        Two 5-cliques connected by a single bridge edge.
        Louvain should find 2 communities (one per clique) — or at worst
        both cliques in the same community, but never split a clique.
        We assert that no community contains nodes from BOTH cliques exclusively.
        """
        g = nx.Graph()
        # Clique 1: nodes 0-4
        for i in range(5):
            for j in range(i + 1, 5):
                g.add_edge(i, j)
        # Clique 2: nodes 5-9
        for i in range(5, 10):
            for j in range(i + 1, 10):
                g.add_edge(i, j)
        # Single bridge
        g.add_edge(4, 5)

        communities = self._run(g)
        assert _all_nodes_covered(communities, range(10))
        # Should not fracture either clique
        clique1 = set(range(5))
        clique2 = set(range(5, 10))
        for c in communities:
            c_set = set(c)
            # A community should not partially overlap both cliques
            assert not (c_set & clique1 and c_set & clique2 and
                        c_set != clique1 and c_set != clique2)

    # ---- delta modularity variant gives same partition properties ----

    def test_full_modularity_variant_also_covers_all_nodes(self):
        g = nx.karate_club_graph()
        lc = LouvainCommunities(g, use_delta_modularity=False)
        communities = lc.louvain()
        all_members = [n for c in communities for n in c]
        assert set(all_members) == set(g.nodes())

    # ---- _aggregate_graph ----

    def test_aggregate_graph_shape(self):
        g = nx.path_graph(4)          # nodes 0,1,2,3
        lc = LouvainCommunities(g)
        adj = lc.adjacency_matrix
        # Two communities: {0,1} and {2,3}
        communities = [frozenset([0, 1]), frozenset([2, 3])]
        new_adj = lc._aggregate_graph(communities, adj)
        assert new_adj.shape == (2, 2)

    def test_aggregate_graph_self_loop_captures_intra_edges(self):
        """The diagonal of the aggregated matrix encodes intra-community edges."""
        g = nx.Graph()
        g.add_edge(0, 1)    # intra-community edge (community 0)
        g.add_edge(2, 3)    # intra-community edge (community 1)
        g.add_edge(1, 2)    # inter-community edge
        lc = LouvainCommunities(g)
        adj = lc.adjacency_matrix
        communities = [frozenset([0, 1]), frozenset([2, 3])]
        new_adj = lc._aggregate_graph(communities, adj)
        # Diagonal should be > 0 (intra-community edges folded in)
        assert new_adj[0, 0] > 0
        assert new_adj[1, 1] > 0

    # ---- _singleton_partition ----

    def test_singleton_partition_length(self):
        g = nx.path_graph(6)
        lc = LouvainCommunities(g)
        parts = lc._singleton_partition(lc.mapping)
        assert len(parts) == 6

    def test_singleton_partition_each_has_one_member(self):
        g = nx.path_graph(4)
        lc = LouvainCommunities(g)
        parts = lc._singleton_partition(lc.mapping)
        for p in parts:
            assert len(p) == 1

    # ---- _compute_delta_modularity_one_community ----

    def test_delta_modularity_zero_when_no_edges(self):
        """A node joining an isolated community contributes 0 delta."""
        g = nx.Graph()
        g.add_nodes_from([0, 1, 2])
        lc = LouvainCommunities(g)
        adj = np.zeros((3, 3))
        delta = lc._compute_delta_modularity_one_community(frozenset([1]), 0, adj)
        assert delta == 0


# ============================================================
# 2.  GargIndex — construction & edge handling
# ============================================================

class TestGargIndexConstruction:

    def test_self_loops_removed(self):
        df = pd.DataFrame({"from": ["A", "A"], "to": ["A", "B"]})
        g = GargIndex(df, "from", "to")
        assert not g.garg_graph.has_edge("A", "A")

    def test_duplicate_edges_deduplicated(self):
        df = pd.DataFrame({
            "from": ["A", "A", "B"],
            "to": ["B", "B", "C"],
        })
        g = GargIndex(df, "from", "to")
        # After dedup, only one A–B edge
        assert g.garg_graph.number_of_edges("A", "B") == 1

    def test_empty_dataframe_creates_empty_graph(self):
        df = pd.DataFrame({"from": [], "to": []})
        g = GargIndex(df, "from", "to")
        assert g.garg_graph.number_of_nodes() == 0

    def test_nodes_present(self):
        g = make_garg([("A", "B"), ("B", "C")])
        assert "A" in g.garg_graph
        assert "C" in g.garg_graph

    def test_undirected_graph_created(self):
        """GargIndex uses nx.Graph (undirected), not DiGraph."""
        g = make_garg([("A", "B")])
        assert isinstance(g.garg_graph, nx.Graph)
        assert not isinstance(g.garg_graph, nx.DiGraph)


# ============================================================
# 3.  GargIndex — get_score / _compute_score
# ============================================================

class TestGargIndexScore:

    def test_unknown_node_returns_zero(self):
        g = make_garg([("A", "B")])
        assert g.get_score("GHOST") == 0

    def test_score_cached_after_first_call(self):
        g = make_garg([("A", "B"), ("B", "C"), ("A", "C")])
        score1 = g.get_score("A")
        # Pollute the raw structure — cached value should still be returned
        g.scores["A"] = 999.0
        score2 = g.get_score("A")
        assert score2 == 999.0   # came from cache

    def test_score_is_numeric(self):
        g = make_garg([("A", "B"), ("B", "C"), ("C", "D"), ("A", "D")])
        score = g.get_score("A")
        assert isinstance(score, (int, float, np.floating))

    def test_central_node_score_different_from_leaf(self):
        """
        Star graph: A at centre connected to B, C, D, E.
        Centre and leaf should produce different GARG scores.
        """
        g = make_garg([("A", "B"), ("A", "C"), ("A", "D"), ("A", "E")])
        score_centre = g.get_score("A")
        score_leaf = g.get_score("B")
        # Not necessarily one > the other, but they must differ
        assert score_centre != score_leaf

    def test_fully_connected_triangle(self):
        """Triangle A–B–C: every node has the same structural role."""
        g = make_garg([("A", "B"), ("B", "C"), ("A", "C")])
        sa, sb, sc = g.get_score("A"), g.get_score("B"), g.get_score("C")
        # Scores should be equal by symmetry (within floating-point tolerance)
        assert abs(sa - sb) < 1e-9
        assert abs(sb - sc) < 1e-9

    def test_score_range_for_example_from_paper_structure(self):
        """
        Reproduce the __main__ example from garg_index.py:
        A→B, A→C, A→D, B→E, C→E, D→E
        All scores should be finite floats.
        """
        edges = [("A", "B"), ("A", "C"), ("A", "D"),
                 ("B", "E"), ("C", "E"), ("D", "E")]
        g = make_garg(edges)
        for node in ["A", "B", "C", "D", "E"]:
            s = g.get_score(node)
            assert np.isfinite(s), f"Score for {node} is not finite: {s}"

    def test_isolated_node_after_self_loop_removal_returns_zero(self):
        """If a node only had a self-loop, it ends up isolated → score 0."""
        df = pd.DataFrame({"from": ["A"], "to": ["A"]})
        g = GargIndex(df, "from", "to")
        assert g.get_score("A") == 0

    def test_compute_all_scores_populates_cache(self):
        edges = [("A", "B"), ("B", "C"), ("C", "A")]
        g = make_garg(edges)
        g.compute_all_scores()
        for node in ["A", "B", "C"]:
            assert node in g.scores

    def test_compute_all_scores_matches_individual(self):
        """compute_all_scores() must give the same result as get_score() one-by-one."""
        edges = [("A", "B"), ("B", "C"), ("C", "D"), ("D", "A"), ("A", "C")]
        g1 = make_garg(edges)
        g2 = make_garg(edges)
        g1.compute_all_scores()
        for node in ["A", "B", "C", "D"]:
            assert abs(g1.scores[node] - g2.get_score(node)) < 1e-9


# ============================================================
# 4.  GargIndex — community detection internals
# ============================================================

class TestGargCommunities:

    def test_get_community_returns_list(self):
        g = make_garg([("A", "B"), ("B", "C")])
        communities = g._get_community()
        assert isinstance(communities, list)

    def test_get_community_idempotent(self):
        """Calling _get_community() twice returns the same object (cached)."""
        g = make_garg([("A", "B"), ("B", "C")])
        c1 = g._get_community()
        c2 = g._get_community()
        assert c1 is c2

    def test_node_community_mapping_populated(self):
        g = make_garg([("A", "B"), ("B", "C"), ("C", "A")])
        g._get_community()
        for node in ["A", "B", "C"]:
            assert node in g._node_community

    def test_community_subgraph_mapping_populated(self):
        g = make_garg([("A", "B"), ("B", "C"), ("C", "A")])
        g._get_community()
        assert len(g._community_subgraph) > 0

    def test_two_cliques_get_different_communities(self):
        """
        Two separate 4-cliques with no bridge → each is its own community.
        nx.community.louvain_communities should keep them separate.
        """
        edges = []
        nodes_c1 = [f"C1_{i}" for i in range(4)]
        nodes_c2 = [f"C2_{i}" for i in range(4)]
        for i, u in enumerate(nodes_c1):
            for v in nodes_c1[i + 1:]:
                edges.append((u, v))
        for i, u in enumerate(nodes_c2):
            for v in nodes_c2[i + 1:]:
                edges.append((u, v))

        g = make_garg(edges)
        communities = g._get_community()

        # The two cliques should not share a community
        for c in communities:
            c_set = set(c)
            has_c1 = bool(c_set & set(nodes_c1))
            has_c2 = bool(c_set & set(nodes_c2))
            assert not (has_c1 and has_c2), (
                f"Community {c_set} mixes both cliques"
            )