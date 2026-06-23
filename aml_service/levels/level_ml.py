from datetime import datetime
import json
import sys
import os


BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__))) # two dirname to get to aml_service path 
    
with open(os.path.join(BASE_DIR, 'config', 'thresholds.json')) as f:
    THRESHOLDS = json.load(f)
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from graph.garg_index import GargIndex

sys.path.append((os.path.dirname(os.path.abspath(__file__))))

from train import get_features

import pandas as pd
import networkx as nx
import joblib
import logging
logging.basicConfig(level=logging.INFO)
class MLLevel:
    def __init__(self, transactions_graph):
        self.logger = logging.getLogger("MLLevel")
        artifact_path = os.path.join(BASE_DIR, 'output', 'lgb_model.joblib')
        artifact = joblib.load(artifact_path)
        self.model = artifact['model']
        self.expected_features = artifact['features']
        self.prediction_threshold = artifact['threshold']
        self.graph = transactions_graph
        df_edges = nx.to_pandas_edgelist(transactions_graph.graph, source="from", target="to")
        self.garg_index = GargIndex(df_edges, key_from="from", key_to="to")
        self.scores = {}
        self.features = None
    
    def _compute_score(self, account):
        return self.garg_index.get_score(account)
    
    def _get_features(self):
        accounts = self.graph.get_accounts()
        cycled_money = dict()
        for account in accounts:
            cycled_money[account] = self.graph.get_money_cycled(account)
        features_df = pd.DataFrame({"Account_ID": accounts,
                           "garg_index": [self._compute_score(account) for account in accounts],
                           })
        features_df["Cycled_Money"] = features_df["Account_ID"].map(cycled_money)
        features_df["Input_Money"] = features_df["Account_ID"].apply(lambda x: self.graph.get_input_money(x))
        features_df["Output_Money"] = features_df["Account_ID"].apply(lambda x: self.graph.get_output_money(x))
        features_df = get_features(features_df, self.graph)
        self.features = features_df
    
    def predict_account(self, account):
        if self.features is None:
            self._get_features()
        features_df = self.features
        account_features = features_df[features_df["Account_ID"] == account].drop(columns=["Account_ID"])
        missing_features = set(self.expected_features) - set(account_features.columns)
        for feature in missing_features:
            self.logger.warning(f"Feature {feature} expected by model but not found in graph features, filling with 0")
            account_features[feature] = 0
        account_features = account_features[self.expected_features]
        score = self.model.predict_proba(account_features)[:, 1][0] ## get the score for the single account
        self.scores[account] = score
        return score >= self.prediction_threshold, score
    def update_graph(self, graph):
        self.graph = graph
        df_edges = nx.to_pandas_edgelist(graph.graph, source="from", target="to")
        self.garg_index = GargIndex(df_edges, key_from="from", key_to="to")
        self.features = None ## reset features to be recalculated with new graph data
    def predict_all(self):
        if self.features is None:
            self._get_features()
        features_df = self.features
        accounts = features_df["Account_ID"]
        accounts_features = features_df.drop(columns=["Account_ID"])
        missing_features = set(self.expected_features) - set(accounts_features.columns)
        for feature in missing_features:
            self.logger.warning(f"Feature {feature} expected by model but not found in graph features, filling with 0")
            accounts_features[feature] = 0
        accounts_features = accounts_features[self.expected_features]
        score = self.model.predict_proba(accounts_features)[:, 1]
        self.scores = dict(zip(accounts, score))
        res = dict()
        for account, score in self.scores.items():
            res[account] = (score >= self.prediction_threshold, score)
        return res

    def get_communities(self):
        return self.garg_index.communities
if __name__ == '__main__':
    from level_graph import *
    transactions = [{
        "from": "BankA_123",
        "to": "BankB_456",
        "amount": 1000,
        "timestamp": datetime.strptime("2024-01-01 10:00:00", "%Y-%m-%d %H:%M:%S")
    }, {
        "from": "BankB_456",
 "to": "BankC_789",
        "amount": 1000,
        "timestamp": datetime.strptime("2024-01-01 11:00:00", "%Y-%m-%d %H:%M:%S")
    }]
    level_graph = TransactionsGraph()
    for transaction in transactions:
        level_graph.add_transaction(transaction["from"], transaction["to"], transaction["amount"], transaction["timestamp"])
    ml_level = MLLevel(level_graph)
    print(ml_level.predict_all())