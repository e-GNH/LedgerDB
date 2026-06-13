import networkx as nx
import numpy as np
class GargIndex:
    def __init__(self, df, key_from, key_to):
        edges = df[[key_from, key_to]]
        edges = edges.drop_duplicates()
        self.garg_graph = nx.from_pandas_edgelist(edges, source=key_from, target=key_to, create_using=nx.Graph())
        self.communities = None
        self.scores = {}
    
    def compute_all_scores(self):
        for account in self.garg_graph.nodes():
            self.get_score(account)
    
    def _get_community(self):
        if self.communities is not None:
            return self.communities
        self.split_graphs = True
        resolution = 1
        if len(self.garg_graph) > 1000:
            resolution = 10
        louvain_communities = nx.community.louvain_communities(self.garg_graph, weight=None, resolution=resolution)
        self.communities = louvain_communities
        return self.communities
    
    def _compute_score(self, account):
        if account not in self.garg_graph:
            return 0
        communities = self._get_community()
        account_community = None
        for community in communities:
            if account in community:
                account_community = community
                break
        if account_community is None:
            return 0
        
        community_graph = self.garg_graph.subgraph(account_community)
        direct_neighbors = set(community_graph.neighbors(account))
        n = len(direct_neighbors)
        second_degree_neighbors = set()
        for neighbor in direct_neighbors:
            second_degree_neighbors.update(community_graph.neighbors(neighbor))
        if account in second_degree_neighbors:
            second_degree_neighbors.remove(account)
        second_degree_neighbors = second_degree_neighbors - direct_neighbors
        m = len(second_degree_neighbors) + 1

        garg_matrix = np.zeros((m + n, m + n))
        matrix_nodes = list()
        matrix_nodes.append(account)
        matrix_nodes.extend(second_degree_neighbors)
        matrix_nodes.extend(direct_neighbors)
        for i, node_i in enumerate(matrix_nodes):
            for j, node_j in enumerate(matrix_nodes):
                if community_graph.has_edge(node_i, node_j):
                    garg_matrix[i, j] = 1
        

        ## defined in paper
        block1 = np.sum(garg_matrix[:m, :m])
        block2 = np.sum(garg_matrix[:m, m:])
        block3 = np.sum(garg_matrix[m:, m:])
        
        score1 = block1 / (m * m - 3 * m + 2 + 1e-10)  ## add small value to avoid division by zero
        score2 = (block2 - n) / (m * n - n + 1e-10)
        score3 = block3 / (n * n - n + 1e-10)
        
        l1 = (m * m - 3 * m + 2)
        l3 = (n * n - n)
        
        score = score2 - (l1 * score1 + l3 * score3) / (l1 + l3 + 1e-10)
        return score

    def get_score(self, account):
        if account not in self.garg_graph:
            return 0
        if account in self.scores:
            return self.scores[account]

        score = self._compute_score(account)
        self.scores[account] = score
        return self.scores[account]
    
if __name__ == "__main__":
    import pandas as pd
    df = pd.DataFrame({
        "from": ["A", "A", "A", "B", "C", "D"],
        "to": ["B", "C", "D", "E", "E", "E"]
    })
    garg_index = GargIndex(df, key_from="from", key_to="to")
    print(garg_index.garg_graph.edges())
    print(garg_index.garg_graph.has_edge('A', 'B'))
    print("Score for A:", garg_index.get_score("A"))