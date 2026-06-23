import networkx as nx

class TransactionsGraph:
    def __init__(self):
        self.graph = nx.MultiDiGraph() ## Multi Directed Graph to allow multiple transactions between the same accounts
        self.LOOP_CUTOFF = 4
        
    def add_transaction(self, from_account, to_account, amount, timestamp):
        if from_account == to_account:
            return ## ignore self loops
        
        self.graph.add_edge(from_account, to_account, amount=amount, timestamp=timestamp)
     
    ## TODO: check function correctness under contention of threads
    def clean_edges(self, cutoff_time):
        edges_to_delete = []
        for edge in self.graph.edges(keys=True, data=True):
            u, v, key, data = edge
            if data["timestamp"] < cutoff_time:
                edges_to_delete.append((u, v, key))
        self.graph.remove_edges_from(edges_to_delete)
        ## remove nodes with no edges from or into
        self.graph.remove_nodes_from(list(nx.isolates(self.graph)))
    
    def get_indegree(self, account):
        if account not in self.graph:
            return 0
        return len(set(self.graph.predecessors(account)))
    
    def get_outdegree(self, account):
        if account not in self.graph:
            return 0
        return len(set(self.graph.successors(account)))
    
    def get_input_money(self, account):
        if account not in self.graph:
            return 0
        sum = 0
        for _, _, data in self.graph.in_edges(account, data=True):
            sum += data["amount"]
        return sum
    
    def get_output_money(self, account):
        if account not in self.graph:
            return 0
        sum = 0
        for _, _, data in self.graph.out_edges(account, data=True):
            sum += data["amount"]
        return sum
    # TODO implement own version of all simple paths with pruning for timestamps to avoid generating all paths and then filtering them, which can be expensive
    # NOTE: This is an approximation and is affected by order of generated paths from networkx
    # Better than generating all paths with combinations of nodes then running full max flow algorithm to respect timing
    def get_money_cycled(self, account):
        ## ignore if account not in graph
        if account not in self.graph:
            return 0
        loops_total_received = 0
        ## Dictionary to keep track of each edge capacity remaining
        remaining_capacity = {}
        for u, v, key, data in self.graph.edges(keys=True, data=True):
            remaining_capacity[(u, v, key)] = data["amount"]
            
        successors = set(self.graph.successors(account))
        for successor in successors:
            successor_allowed_flow = sum([data["amount"] 
                                        for _, v, data in self.graph.out_edges(account, data=True) 
                                        if v == successor])
            ## This will be fixed in new implementation
            ## This will catch false positives where there are multiple transactions from account to successor
            # but only one of them is part of a loop. 
            # but it is okay to have false positives here because we care more about recall than precision in AML
            # Case: A->B 1000 B->A 1500 A->B 500, this will return 1500 as amount cycled back 
            # but it is better than missing other cases where A->B 1000 A->B 500 B->A 1500 which needs 1500 to be returned
            original_successor_timestamp = min([data["timestamp"] 
                                        for u, v, data in self.graph.out_edges(account, data=True) 
                                        if v == successor])
            
            if nx.has_path(self.graph, successor, account):
                paths = nx.all_simple_edge_paths(self.graph, successor, account, cutoff = self.LOOP_CUTOFF)
                paths = sorted(paths, key=len)
                for path in paths:
                    cycle_back_limit = successor_allowed_flow
                    max_path_timestamp = original_successor_timestamp
                    for u, v, key in path:
                        edge_data = self.graph.get_edge_data(u, v, key=key)
                        if edge_data["timestamp"] < max_path_timestamp:
                            cycle_back_limit = 0
                            break ## can't go back in time between two edges
                        max_path_timestamp = edge_data["timestamp"]
                        cycle_back_limit = min(cycle_back_limit, remaining_capacity[(u, v, key)])
                    
                        
                    loops_total_received += cycle_back_limit
                    successor_allowed_flow -= cycle_back_limit
                    for u, v, key in path:
                          remaining_capacity[(u, v, key)] -= cycle_back_limit
                    if successor_allowed_flow <= 0:
                        break
        return loops_total_received
                    
                    
    
    def get_nodes(self):
        return len(self.graph)
    
    def fast_get_money_cycled(self, account):
        """
        Calculates total money cycled back to from an account within a certain path length threshold
        only moves in DFS if time is non-decreasing along the path and keeps track of remaining capacity per edge
        """
                ## ignore if account not in graph
        if account not in self.graph:
            return 0
        loops_total_received = 0
        ## Dictionary to keep track of each edge capacity remaining
        remaining_capacity = {}            
        
        def dfs(current_node, target, current_length, current_path_limit, current_path, current_timestamp, visited):
            if current_node == target:
                
                for u, v, key in current_path:
                    remaining_capacity[(u, v, key)] -= current_path_limit
                
                return current_path_limit
            if current_length >= self.LOOP_CUTOFF or current_path_limit <= 0:
                return 0
            
            edges = self.graph.out_edges(current_node,data=True, keys=True)
            ## U, V, Key, Data
            edges = sorted(edges, key = lambda edge: edge[3]["timestamp"]) ## get earliest timestamps first
            total_cycled = 0
            for u, v, key, data in edges:
                if v in visited:
                    continue
                if (u, v, key) not in remaining_capacity:
                    remaining_capacity[(u, v, key)] = data["amount"]
                if data["timestamp"] < current_timestamp:
                    continue
                limit = min(remaining_capacity[(u, v, key)], current_path_limit)
                current_path.append((u, v, key))
                visited.add(v)
                used = dfs(v, target, current_length + 1, limit, current_path, data["timestamp"], visited)
                visited.remove(v)
                current_path_limit -= used
                total_cycled += used
                current_path.pop()
            return total_cycled
            
            
        
        for _, successor, key, data in self.graph.out_edges(account, keys=True, data=True):
            path = list([(account, successor, key)])
            if (account, successor, key) not in remaining_capacity:
                remaining_capacity[(account, successor, key)] = data["amount"]
            cycled_from_edge = dfs(successor, account, 1, data["amount"], path, data["timestamp"], visited = set([successor]))
            loops_total_received += cycled_from_edge
            
        return loops_total_received
    
    def get_accounts(self):
        return self.graph.nodes()
    
    def check_scatter_gather(self, account, levels, threshold_scatter = 3):
        if account not in self.graph:
            return list(), 0
        
        current_dict = dict()
        
        for successor in self.graph.successors(account):
            edges = self.graph.get_edge_data(account, successor)
            timestamp = min(edges[edge_key]["timestamp"] for edge_key in edges)
            current_dict[successor] = [1, timestamp]
        
        if len(current_dict) == 0:
            return list(), 0
        for _ in range(levels):
            
            if len(current_dict) < threshold_scatter:
                max_value = 0
                if len(current_dict):
                    max_value = max(current_dict[acc][0] for acc in current_dict)
                res = list()
                for acc in current_dict:
                    if current_dict[acc][0] == max_value:
                        res.append(acc)
                return res, max_value
            prev_dict = current_dict
            current_dict = dict()
            
            for successor in prev_dict:
                successors = self.graph.successors(successor)
                successor_val, successor_timestamp = prev_dict[successor]
                for next_successor in successors:
                    edges = self.graph.get_edge_data(successor, next_successor)
                    max_timestamp = max(edges[edge_key]["timestamp"] for edge_key in edges)
                    ### the gather was after scatter
                    if max_timestamp < successor_timestamp:
                        continue
                    timestamp = min(edges[edge_key]["timestamp"] for edge_key in edges)
                    if next_successor in current_dict:
                        current_dict[next_successor][0] = current_dict[next_successor][0] + successor_val
                        current_dict[next_successor][1] = min(current_dict[next_successor][1], timestamp)
                    else:
                        current_dict[next_successor] = [successor_val, timestamp]
        if len(current_dict):
            max_value = max(current_dict[acc][0] for acc in current_dict) 
        else:
            max_value = 0
        res = list()
        
        for acc in current_dict:
            if current_dict[acc][0] == max_value:
                res.append(acc)
        return res, max_value
    
    def check_bipartite_subgraph(self, community, minimum_graph_size = 5):
        accounts = set()
        for acc in community:
            if acc in self.graph:
                accounts.add(acc)
        if len(accounts) < minimum_graph_size:
            return False
        subgraph = nx.subgraph(self.graph, accounts)
        return nx.is_bipartite(subgraph)