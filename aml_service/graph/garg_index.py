import networkx as nx
import numpy as np

class LouvainCommunities():
    def __init__(self, graph, resolution = 1, use_weight = False, use_delta_modularity = True):
        self.graph = graph
        self.resolution = resolution ## For controlling size of communities
        self.mapping = dict()
        i = 0
        for node in graph:
            self.mapping[i] = node
            i += 1
        
        weight = 'weight' if use_weight else None
        self.adjacency_matrix = nx.adjacency_matrix(graph, weight=weight).toarray()
        self.cached_modularities = dict()
        self.use_delta_modularity = use_delta_modularity
        
        self.MAX_ITERATIONS = 1000
    
    def _compute_delta_modularity_one_community(self, community, node_new, adjacency_matrix):
        #TODO: Implement and use instead of caching
        m = np.sum(adjacency_matrix) / 2 #divide by 2 since undirected graph
        if m == 0:
            print("edges weigth in graphs = 0")
            return 0
        community_modularity = 0 
        ## edges between new node and community members
        K_i_in = 0
        for node_i in community:
            K_i_in += adjacency_matrix[node_i][node_new]
        
        positive_term = K_i_in / (2 * m)
        
        K_i = np.sum(adjacency_matrix[node_new])
        
        E_tot = 0
        for node in community:
            E_tot += np.sum(adjacency_matrix[node])
            
        subtraction_term = E_tot * K_i / (2 * m * m)

        return positive_term - subtraction_term * self.resolution
    
    def _compute_delta_modularity_movement(self, community_old, community_new, node_new, adjacency_matrix):
        temp_new = set(community_new.copy())
        
        if node_new in temp_new:
            temp_new.remove(node_new)
        
        positive_change = self._compute_delta_modularity_one_community(temp_new, node_new, adjacency_matrix)
        temp_old = set(community_old.copy())
        
        if node_new in temp_old:
            temp_old.remove(node_new)
        negative_change = self._compute_delta_modularity_one_community(temp_old, node_new, adjacency_matrix)
        return positive_change - negative_change
        
    
    def _compute_modularity(self, communities, adjacency_matrix):
        total_modularity = 0
        
        # m = sum of edges
        m = np.sum(adjacency_matrix) / 2 #divide by 2 since undirected graph
        if m == 0:
            print("edges weigth in graphs = 0")
            return 0
        for community in communities:
            community_modularity = 0
            if community in self.cached_modularities:
                community_modularity = self.cached_modularities[community]
            else:
                for node_i in community:
                    for node_j in community:
                        community_modularity += adjacency_matrix[node_i][node_j]
                
                community_modularity /= (2 * m)
                
                subtraction_term = 0
                for node in community:
                    subtraction_term += np.sum(adjacency_matrix[node])
                    
                subtraction_term /= (2 * m)
                subtraction_term = subtraction_term ** 2
                
                community_modularity = community_modularity - subtraction_term * self.resolution
                self.cached_modularities[frozenset(community)] = community_modularity
                
            total_modularity += community_modularity
        return total_modularity

    def _move_nodes(self, communities, adjacency_matrix):

        old_modularity = 0
        curr_modularity = 0        
        initial_iteration = True
        curr_modularity = self._compute_modularity(communities, adjacency_matrix)
        change = curr_modularity - old_modularity
        epsilon = 1e-5
        iteration = 0
        while (change > epsilon or initial_iteration) and iteration < self.MAX_ITERATIONS:
            iteration += 1
            if(iteration == self.MAX_ITERATIONS):
                print("Louvain matrix loop reached max iterations")

            initial_iteration = False
            old_modularity = curr_modularity = self._compute_modularity(communities, adjacency_matrix)
            
            ## Move nodes ## loop by order of communities
            visited = set()
            for i in range(len(communities)):
                community = communities[i]
                for node_index in community:
                    if node_index in visited:
                        continue
                    visited.add(node_index)
                    # try removing it
                    best_idx = -1
                    best_modularity = curr_modularity
                    community_set = set(communities[i])
                    community_set.remove(node_index)
                    communities[i] = frozenset(community_set)
                    for j in range(len(communities)):
                        if i == j:
                            continue
                        ## try adding it
                        community_j = communities[j]
                        comm_j_set = set(community_j)
                        comm_j_set.add(node_index)
                        communities[j] = frozenset(comm_j_set)
                        if self.use_delta_modularity:
                            change_from_curr = self._compute_delta_modularity_movement(community_set, comm_j_set, node_index,adjacency_matrix)
                            new_modularity = curr_modularity + change_from_curr
                            change_from_best_modularity = new_modularity - best_modularity
                            if change_from_best_modularity > epsilon:
                                best_idx = j
                                best_modularity = new_modularity
                                
                        else:
                            new_modularity = self._compute_modularity(communities, adjacency_matrix)
                            change_from_best_modularity = new_modularity - best_modularity
                            if change_from_best_modularity > epsilon:
                                best_idx = j
                                best_modularity = new_modularity
                        communities[j] = community_j

                    if best_modularity - curr_modularity > epsilon:
                        community_dst = communities[best_idx]
                        comm_dst_set = set(community_dst)
                        comm_dst_set.add(node_index)
                        communities[best_idx] = frozenset(comm_dst_set)
                        curr_modularity = best_modularity
                    else:
                        communitiy_orig = communities[i]
                        community_orig_set = set(communitiy_orig)
                        community_orig_set.add(node_index)
                        communities[i] = frozenset(community_orig_set)
            change = curr_modularity - old_modularity            
            communities = [community for community in communities if len(community)]         

        return communities            
            
    def _aggregate_graph(self, communities, adjacency_matrix):
        n = len(communities)
        new_adjacency_matrix = np.zeros((n, n))
        for i in range(n):
            ## add self loops
            for node_i in communities[i]:
                for node_j in communities[i]:
                    new_adjacency_matrix[i, i] += adjacency_matrix[node_i, node_j]
                    
            for j in range(i + 1, n):
                for node_i in communities[i]:
                    for node_j in communities[j]:
                        new_adjacency_matrix[i, j] += adjacency_matrix[node_i, node_j]
                        new_adjacency_matrix[j, i] += adjacency_matrix[node_i, node_j]
        return new_adjacency_matrix
    
    def _singleton_partition(self, mapping):
        ## Initially one node per community
        communities = list()
        for node_index in mapping:
            communities.append(frozenset([node_index]))
        return communities
    
    def _flatten_clusters(self, new_communities, mappings):
        levels = len(mappings)
        finalized_clusters = list()
        for community_idx in range(len(new_communities)):
            curr_level_community = new_communities[community_idx]

            for level in range(levels):
                higher_community = list()
                curr_level = levels - 1 - level
                curr_mapping = mappings[curr_level]

                for item in curr_level_community:
                    if curr_level == 0:
                        higher_community.append(curr_mapping[item])
                    else:
                        higher_community.extend(list(curr_mapping[item]))

                curr_level_community = higher_community
            finalized_clusters.append(curr_level_community)
            
        return finalized_clusters
                
    
    def louvain(self):
        done = False
        initial_communities = self._singleton_partition(self.mapping)
        mappings = list()
        mappings.append(self.mapping)
        adjacency_matrix = self.adjacency_matrix
        while not done:
            # reset cached_modularities for this level
            self.cached_modularities = dict()
            
            new_communities = self._move_nodes(initial_communities, adjacency_matrix)
            done = len(new_communities) == len(initial_communities)
            
            if not done:
                new_mapping = dict()
                for i in range(len(new_communities)):
                    new_mapping[i] = new_communities[i]
                mappings.append(new_mapping)
                adjacency_matrix = self._aggregate_graph(new_communities, adjacency_matrix)
                initial_communities = self._singleton_partition(new_mapping)
                
        return self._flatten_clusters(new_communities, mappings)
            
            
class GargIndex:
    def __init__(self, df, key_from, key_to):
        edges = df[[key_from, key_to]]
        edges = edges[edges[key_from] != edges[key_to]]  ## remove self loops
        edges = edges.drop_duplicates()
        self.garg_graph = nx.from_pandas_edgelist(edges, source=key_from, target=key_to, create_using=nx.Graph())
        self.communities = None
        self.scores = {}
        self.use_networkx_louvain = True
    
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
        if self.use_networkx_louvain:
            louvain_communities = nx.community.louvain_communities(self.garg_graph, weight=None, resolution=resolution)
            self.communities = louvain_communities
        else:
            louvain_object = LouvainCommunities(self.garg_graph, 1)
            louvain_communities = louvain_object.louvain()
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