import networkx as nx

class TransactionsGraph:
    def __init__(self):
        self.graph = nx.MultiDiGraph() ## Multi Directed Graph to allow multiple transactions between the same accounts
    
    def add_transaction(self, from_account, to_account, amount, timestamp):
        self.graph.add_edge(from_account, to_account, amount=amount, timestamp=timestamp)
        
    def clean_edges(self, cutoff_time):
        edges_to_be_deleted = [ (from_account, to_account, key)  ## key to delete edge in MultiDiGraph needs to be identified because multiple edges can exist between same nodes
                               for from_account, to_account, key, data in 
                               self.graph.edges(keys=True, data=True) 
                               if data['timestamp'] < cutoff_time]
        self.graph.remove_edges_from(edges_to_be_deleted)
        ## remove isolated nodes (has no edges into or out of)
        self.graph.remove_nodes_from(list(nx.isolates(self.graph)))
    
    def get_indegree(self, account):
        return self.graph.in_degree(account)
    
    def get_outdegree(self, account):
        return self.graph.out_degree(account)
    
    def get_input_money(self, account):
        return sum(data['amount'] for _, _, data in self.graph.in_edges(account, data=True))
    
    def get_output_money(self, account):
        return sum(data['amount'] for _, _, data in self.graph.out_edges(account, data=True))