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

        old_modularity = float('-inf')
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
        self.edges = df[[key_from, key_to]]
        self.edges = self.edges[df[key_from] != df[key_to]] ## remove self loops
        self.edges = self.edges.drop_duplicates()
        self.use_networkx_louvain = True
        self.garg_graph = nx.from_pandas_edgelist(self.edges, key_from, key_to, create_using = nx.Graph())
        self.communities = None
        self.scores = dict()
        self._node_community = dict()
        self._community_subgraph = dict()
        self.weight = None
    
    def compute_all_scores(self):
        for node in self.garg_graph:
            score = self._compute_score(node)
            self.scores[node] = score
        
        
    def _get_community(self):
        if self.communities is not None:
            return self.communities
        
        resolution = 1
        if self.garg_graph.number_of_nodes() > 1000:
            resolution = 10
        if self.use_networkx_louvain:
            communities = nx.community.louvain_communities(self.garg_graph, self.weight, resolution)
            self.communities = communities
        else:
            use_weight = True if self.weight is not None else False
            louvainObject = LouvainCommunities(self.garg_graph, resolution, use_weight)
            communities = louvainObject.louvain()
            self.communities = communities
        
        for community in self.communities:
            for node in community:
                self._node_community[node] = frozenset(community)
        
            self._community_subgraph[frozenset(community)] = self.garg_graph.subgraph(community)
            
        return self.communities
        
        
    def _compute_score(self, account):
        if account not in self.garg_graph:
            return 0
        
        communities = self._get_community()
        
        if account not in self._node_community:
            print(f"Account {account} has no community in cache")
            return 0
        account_community = frozenset(self._node_community[account])
        
        if account_community not in self._community_subgraph:
            print(f"Account community {account} has no subgraph in cache")
            return 0
        community_subgraph = self._community_subgraph[account_community]
        
        direct_neighbors = set(community_subgraph.neighbors(account))
        direct_neighbors.discard(account)
        
        n = len(direct_neighbors)
        second_degree_neighbors = set()
        for neighbor in direct_neighbors:
            second_degree_neighbors.update(community_subgraph.neighbors(neighbor))
            
        second_degree_neighbors.difference_update(direct_neighbors)
        second_degree_neighbors.discard(account)
        
        m = len(second_degree_neighbors) + 1
        
        garg_matrix = np.zeros((n + m, n + m))
        
        nodes = list()
        nodes.append(account)
        for second_degree_neighbor in second_degree_neighbors:
            nodes.append(second_degree_neighbor)
        for neighbor in direct_neighbors:
            nodes.append(neighbor)       
        
        index_dict = {}
        index = 0
        for node in nodes:
            index_dict[node] = index
            index += 1
        
        for i in range(len(nodes)):
            for neighbor in community_subgraph.neighbors(nodes[i]):
                if neighbor not in index_dict:
                    continue
                node_index = index_dict[neighbor]
                if node_index == i:
                    continue
                if community_subgraph.has_edge(nodes[i], nodes[node_index]):
                    garg_matrix[i][node_index] = 1
    

        ## add small value to avoid division by zero
        epsilon = 1e-10
        
        ## equations defined in paper
        block1_sum = np.sum(garg_matrix[:m, :m])
        block2_sum = np.sum(garg_matrix[:m, m:m + n])
        block3_sum = np.sum(garg_matrix[m:m + n, m:m + n])
    
        score1 = block1_sum / (epsilon + m ** 2 - 3 * m + 2)
        score2 = (block2_sum - n) / (epsilon + m * n - n)
        score3 = (block3_sum) / (epsilon + n ** 2 - n)
        
        l1 = m ** 2 - 3 * m + 2
        l3 = n ** 2 - n
        
        score = score2 - (l1 * score1 + l3 * score3) / (epsilon + l1 + l3)
        
        return score
    

    def get_score(self, account):
        if account not in self.garg_graph:
            return 0
        
        if account in self.scores:
            return self.scores[account]
        
        score = self._compute_score(account)
        self.scores[account] = score
        return self.scores[account]

# TODO: DELETE    
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
    print("Score for B:", garg_index.get_score("B"))
    print("Score for C:", garg_index.get_score("C"))
    print("Score for D:", garg_index.get_score("D"))
    print("Score for E:", garg_index.get_score("E"))
    print("Score for F:", garg_index.get_score("F"))