import json
from collections import defaultdict
import os, sys
from datetime import timedelta
from datetime import datetime

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


with open(os.path.join(BASE_DIR, "config", "thresholds.json"), 'r') as f:
    THRESHOLDS = json.load(f)
    
sys.path.append(BASE_DIR)

from graph.garg_index import GargIndex
import pandas as pd
import networkx as nx
from train import get_features
import joblib
import logging

class MLLevel:
    def __init__(self, transactions_graph):
        self.graph = transactions_graph
        artifact_path = os.path.join(BASE_DIR, "output", "lgb_model.joblib")
        df = nx.to_pandas_edgelist(transactions_graph.graph, "from", "to")
        self.garg = GargIndex(df, "from", "to")
        artifact = joblib.load(artifact_path)
        self.model = artifact["model"]
        self.threshold = artifact["threshold"]
        self.expected_features = artifact["features"]
        self.features = None
        self.scores = dict()
    
    def _compute_score(self, account):
        return self.garg.get_score(account)
    
    def _get_features(self):
        accounts = self.graph.get_accounts()
        garg_scores = dict()
        for account in accounts:
            garg_score = self._compute_score(account)
            garg_scores[account] = garg_score
        df_dict = {
            "Account_ID" : garg_scores.keys(),
            "garg_index": [garg_scores[key] for key in garg_scores.keys()]
        }   
        df = pd.DataFrame.from_dict(df_dict)
        df["Cycled_Money"] = df["Account_ID"].apply(lambda x: self.graph.fast_get_money_cycled(x))
        df["Input_Money"] = df["Account_ID"].apply(lambda x: self.graph.get_input_money(x))
        df["Output_Money"] = df["Account_ID"].apply(lambda x: self.graph.get_output_money(x))
        df = get_features(df, self.graph)
        self.features = df

    
    def predict_account(self, account):
        if self.features is None:
            self._get_features()
        features = self.features
        accounts = features["Account_ID"].unique()
        if account not in accounts:
            logging.warning("Account not in graph")
            return 0
        account_features = features[features["Account_ID"] == account].drop(["Account_ID"], axis = 1)
        current_features = set(account_features.columns)
        for feature in self.expected_features:
            if feature not in current_features:
                logging.warning(f"feature {feature} not in dataframe for prediction")
                account_features[feature] = 0
        account_features = account_features[self.expected_features]
        prediction_proba = self.model.predict_proba(account_features)[0, 1]
        self.scores[account] = prediction_proba > self.threshold, prediction_proba

        return prediction_proba > self.threshold, prediction_proba
    
    def update_graph(self, graph):
        self.features = None
        self.graph = graph
        df = nx.to_pandas_edgelist(graph.graph, "from", "to")
        self.garg = GargIndex(df, "from", "to")
        self.scores = dict()
        return
    
    def predict_all(self):
        if self.features is None:
            self._get_features()
        res = dict()
        features = self.features
        accounts = features["Account_ID"].unique()
        account_features = features.drop(["Account_ID"], axis = 1)
        current_features = set(account_features.columns)
        for feature in self.expected_features:
            if feature not in current_features:
                logging.warning(f"feature {feature} not in dataframe for prediction")
                account_features[feature] = 0
        account_features = account_features[self.expected_features]
        prediction_proba = self.model.predict_proba(account_features)[:, 1]

        for i in range(len(accounts)):
            account = accounts[i]
            res[account] = (prediction_proba[i] > self.threshold, prediction_proba[i])
            self.scores[account] = res[account]
        return res
    
    def get_communities(self):
        return self.garg.communities