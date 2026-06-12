import networkx as nx

class TransactionsGraph:
    def __init__(self):
        self.graph = nx.MultiDiGraph() ## Multi Directed Graph to allow multiple transactions between the same accounts
        self.LOOP_CUTOFF = 4
        
    def add_transaction(self, from_account, to_account, amount, timestamp):
        self.graph.add_edge(from_account, to_account, amount=amount, timestamp=timestamp)
     
    ## TODO: CALL that asynchronously with locks
    def clean_edges(self, cutoff_time):
        edges_to_be_deleted = [ (from_account, to_account, key)  ## key to delete edge in MultiDiGraph needs to be identified because multiple edges can exist between same nodes
                               for from_account, to_account, key, data in 
                               self.graph.edges(keys=True, data=True) 
                               if data['timestamp'] < cutoff_time]
        self.graph.remove_edges_from(edges_to_be_deleted)
        ## remove isolated nodes (has no edges into or out of)
        self.graph.remove_nodes_from(list(nx.isolates(self.graph)))
    
    def get_indegree(self, account):
        return len(set(self.graph.predecessors(account)))
    
    def get_outdegree(self, account):
        return  len(set(self.graph.successors(account)))
    
    def get_input_money(self, account):
        return sum(data['amount'] for _, _, data in self.graph.in_edges(account, data=True))
    
    def get_output_money(self, account):
        return sum(data['amount'] for _, _, data in self.graph.out_edges(account, data=True))
    
    # TODO implement own version of all simple paths with pruning for timestamps to avoid generating all paths and then filtering them, which can be expensive
    # NOTE: This is an approximation and is affected by order of generated paths from networkx
    # Better than generating all paths with combinations of nodes then running full max flow algorithm to respect timing
    def get_money_cycled(self, account):
                
        loops_total_received = 0
        remaining_capacity = {}
        for u, v, key, data in self.graph.edges(keys=True, data=True):
            remaining_capacity[(u, v, key)] = data['amount']
            
        for successor in set(self.graph.successors(account)): ## check each unique successor to avoid redundant path calculations
            if nx.has_path(self.graph,successor, account): ## check if there is a path back to the original account
                simple_paths = sorted(
                    list(nx.all_simple_edge_paths(self.graph, source=successor, target=account, cutoff=self.LOOP_CUTOFF))
                    , key = len)
                successor_min_amount = sum(data['amount'] for _, dest, data in self.graph.out_edges(account, data=True) if dest == successor)
                ## This will catch false positives where there are multiple transactions from account to successor
                # but only one of them is part of a loop. 
                # but it is okay to have false positives here because we care more about recall than precision in AML
                # Case: A->B 1000 B->A 1500 A->B 500, this will return 1500 as amount cycled back 
                # but it is better than missing other cases where A->B 1000 A->B 500 B->A 1500 which needs 1500 to be returned
                successor_timestamp = min(data['timestamp'] for _, dest, data in self.graph.out_edges(account, data=True) if dest == successor)
                remaining_source_capacity = successor_min_amount
                for path in simple_paths:
                    if remaining_source_capacity <= 0:
                        break
                    min_amount = remaining_source_capacity ## initialize min_amount with the amount of the first transaction from account to successor, as this is the maximum amount that can be cycled back through this path
                    timestamp = successor_timestamp ## initialize timestamp with the timestamp of the first transaction from account to successor
                    for u, v, key in path:
                        edge_data = self.graph.get_edge_data(u, v, key=key)
                        if edge_data['timestamp'] >= timestamp:
                            min_amount = min(min_amount, remaining_capacity[(u, v, key)])
                            timestamp = edge_data['timestamp']
                        else:
                            min_amount = 0
                            break ## if we encounter an edge with timestamp older than the initial transaction, we can stop checking this path as it won't contribute to the loop amount
                    remaining_source_capacity -= min_amount
                    loops_total_received += min_amount
                    
                    for u, v, key in path:
                        remaining_capacity[(u, v, key)] -= min_amount
        
        return loops_total_received
    
    def get_nodes(self):
        return self.graph.number_of_nodes()
    
    def fast_get_money_cycled(self, account):
        remaining_capacity = {}
        def dfs(node, target, path_length_threshold, curr_timestamp, paths, current_path):
            if path_length_threshold <= 0:
                return
            if node == target:
                paths.append(list(current_path))
                return
            for edge in self.graph.out_edges(node, data=True, keys=True):
                u, v, key, data = edge
                if ((u, v, key) not in remaining_capacity):
                    remaining_capacity[(u, v, key)] = data['amount']
                if curr_timestamp == 0 or data['timestamp'] >= curr_timestamp:
                    current_path.append((u , v, key))
                    dfs(v, target, path_length_threshold - 1, data['timestamp'], paths, current_path)
                    current_path.pop()     
            return
        
        loops_total_received = 0
        for successor in set(self.graph.successors(account)): 
                simple_paths = []
                dfs(successor, account, self.LOOP_CUTOFF, 0, simple_paths, [])
                simple_paths = sorted(
                    list(simple_paths)
                    , key = len)
                successor_min_amount = sum(data['amount'] for _, dest, data in self.graph.out_edges(account, data=True) if dest == successor)
                ## This will catch false positives where there are multiple transactions from account to successor
                # but only one of them is part of a loop. 
                # but it is okay to have false positives here because we care more about recall than precision in AML
                # Case: A->B 1000 B->A 1500 A->B 500, this will return 1500 as amount cycled back 
                # but it is better than missing other cases where A->B 1000 A->B 500 B->A 1500 which needs 1500 to be returned
                successor_timestamp = min(data['timestamp'] for _, dest, data in self.graph.out_edges(account, data=True) if dest == successor)
                remaining_source_capacity = successor_min_amount
                for path in simple_paths:
                    if remaining_source_capacity <= 0:
                        break
                    min_amount = remaining_source_capacity ## initialize min_amount with the amount of the first transaction from account to successor, as this is the maximum amount that can be cycled back through this path
                    timestamp = successor_timestamp ## initialize timestamp with the timestamp of the first transaction from account to successor
                    for u, v, key in path:
                        edge_data = self.graph.get_edge_data(u, v, key=key)
                        if edge_data['timestamp'] >= timestamp:
                            min_amount = min(min_amount, remaining_capacity[(u, v, key)])
                            timestamp = edge_data['timestamp']
                        else:
                            min_amount = 0
                            break ## if we encounter an edge with timestamp older than the initial transaction, we can stop checking this path as it won't contribute to the loop amount
                    remaining_source_capacity -= min_amount
                    loops_total_received += min_amount
                    for u, v, key in path:
                        remaining_capacity[(u, v, key)] -= min_amount
        return loops_total_received