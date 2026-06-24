import json
from collections import defaultdict
import os, sys
from datetime import timedelta
from datetime import datetime

BASE_DIR = os.path.dirname((os.path.abspath(__file__)))


sys.path.append(BASE_DIR)
from graph.builder import TransactionsGraph
from graph.garg_index import GargIndex
import pandas as pd
import networkx as nx
import joblib
import logging
logging.basicConfig(level=logging.INFO)
import numpy as np
import lightgbm as lgb
from sklearn.model_selection import train_test_split, StratifiedKFold
from sklearn.metrics import classification_report, confusion_matrix, roc_auc_score, average_precision_score, precision_recall_curve

from sklearn.ensemble import RandomForestClassifier
from sklearn.model_selection import GridSearchCV, RandomizedSearchCV
from sklearn.utils.class_weight import compute_class_weight
CURRENCY_EXCHANGE = {
    "US Dollar": 52,
    "Bitcoin": 3314084,
    "Euro": 60,
    "Australian Dollar": 36.5,
    "Yuan": 7.67,
    "Rupee": 0.55,
    "Yen": 0.32,
    "Mexican Peso": 3,
    "UK Pound": 69.75,
    "Ruble": 0.72,
    "Canadian Dollar": 37.19,
    "Swiss Franc": 65.27,
    "Brazil Real": 10.24,
    "Saudi Riyal": 13.84,
    "Shekel": 17.8
}

def read_csv(file_path):
    if not os.path.exists(file_path):
        logging.error(f"File {file_path} doesn't exist")
        return None
    logging.info(f"Reading csv from {file_path}")
    df = pd.read_csv(file_path)
    return df

def load_data(transactions_path, cycled_money_path, garg_index_path):
    logging.info("Starting data extraction")
    df_transactions = read_csv(transactions_path)
    df_cycled_money = read_csv(cycled_money_path)
    df_garg_index = read_csv(garg_index_path)
    logging.info("Finished data extraction")
    return df_transactions, df_cycled_money, df_garg_index

def clean_transactions_df(df_transactions):
    # GET from account
    df_transactions["From_Account"] = df_transactions["From Bank"].astype(str) + "_" + \
    df_transactions["Account"].astype(str)
    # GET TO
    df_transactions["To_Account"] = df_transactions["To Bank"].astype(str) + "_" + \
    df_transactions["Account.1"].astype(str)
    # GET EGP amount
    df_transactions["Amount_Paid_EGP"] = df_transactions.apply(lambda row:
    row["Amount Paid"] * CURRENCY_EXCHANGE[row["Payment Currency"]],
    axis = 1
    )
    # DROP USELESS columns
    df_transactions = df_transactions.drop(columns = ["To Bank", "Account", "From Bank", "Account.1", "Amount Paid", "Payment Currency"])
    # GET timestamps
    df_transactions["Timestamp"] = pd.to_datetime(df_transactions["Timestamp"])
    return df_transactions

def get_laundering_df(df_transactions):
    laundering_transactions = df_transactions[df_transactions["Is Laundering"] == 1]
    laundering_accounts = set(laundering_transactions["From_Account"].unique())
    laundering_accounts = laundering_accounts.union(set(laundering_transactions["To_Account"].unique()))
    all_accounts = set(df_transactions["From_Account"].unique())
    all_accounts = all_accounts.union(set(df_transactions["To_Account"].unique()))
    all_accounts = list(all_accounts)
    df_accounts = pd.DataFrame(
    {
    "Account_ID": all_accounts,
    "is_laundering": [int(account in laundering_accounts) for account in all_accounts],
    }
    )
    return df_accounts

def populate_graph(df_transactions):
    graph = TransactionsGraph()
    total = len(df_transactions)
    for i, row in df_transactions.iterrows():
        if i % 100000 == 0:
            logging.info(f"processed {i} transactions out of {total}")
        from_acc = row["From_Account"]
        to_acc = row["To_Account"]
        amount = row["Amount_Paid_EGP"]
        timestamp = row["Timestamp"]
        graph.add_transaction(from_acc, to_acc, amount, timestamp)
    return graph

def merge_extracted_features(df_accounts, df_cycled_money, df_garg_index):
    merged_df = pd.merge(df_accounts, df_cycled_money, on="Account_ID")
    merged_df = pd.merge(merged_df, df_garg_index, on="Account_ID")
    return merged_df

def save_data(df_data, output_path):
    df_data.to_csv(output_path, index = False)
    logging.info(f"Saved data into {output_path}")
    return

def get_features(df_data, graph):
    epsilon = 1e-9
    df_data["Input_Output_Ratio"] = df_data["Input_Money"] / (df_data["Output_Money"] + epsilon)
    df_data["Cycled_Input_Ratio"] = df_data["Cycled_Money"] / (df_data["Output_Money"] + epsilon)
    df_data["Cycled_Output_Ratio"] = df_data["Cycled_Money"] / (df_data["Input_Money"] + epsilon)
    df_data["indegree"] = df_data["Account_ID"].apply(lambda account: graph.get_indegree(account))
    df_data["outdegree"] = df_data["Account_ID"].apply(lambda account: graph.get_outdegree(account))
    ##. outdegree & indegree from networkx returns number of out edges which isn't outdegree that we calculate since we have multidigraph
    df_data["Transactions_Out"] = df_data["Account_ID"].apply(lambda account: graph.graph.out_degree(account) if account in graph.graph else 0)
    df_data["Transactions_In"] = df_data["Account_ID"].apply(lambda account: graph.graph.in_degree(account) if account in graph.graph else 0)
    df_data["Avg_TX_in"] = df_data["Input_Money"] / (df_data["Transactions_In"] + epsilon)
    df_data["Avg_TX_out"] = df_data["Output_Money"] / (df_data["Transactions_Out"] + epsilon)
    df_data["Transactions_Per_Input"] = df_data["Transactions_In"] / (df_data["indegree"] + epsilon)
    df_data["Transactions_Per_Output"] = df_data["Transactions_Out"] / (df_data["outdegree"] + epsilon)
    return df_data

def save_artifacts(model, output_path, features, threshold):
    artifacts = {
    "model": model,
    "features": features,
    "threshold": threshold
    }
    joblib.dump(artifacts, output_path)
    logging.info(f"saved model artifacts to {output_path}")
    return

def evaluate_model(model, X_train, y_train, X_val, y_val, threshold=0.3):
    logging.info(f"Performance on training set")
    y_pred_train_proba = model.predict_proba(X_train)[:, 1]
    y_pred_train = y_pred_train_proba > threshold
    logging.info(classification_report(y_train, y_pred_train))
    logging.info(confusion_matrix(y_train, y_pred_train))

    logging.info(f"Performance on val set")
    y_pred_val_proba = model.predict_proba(X_val)[:, 1]
    logging.info(f"ROC AUC Score {roc_auc_score(y_val, y_pred_val_proba)}")
    logging.info(f"Average Precision Score {average_precision_score(y_val, y_pred_val_proba)}")
    logging.info(f"Performance on val set after using threshold {threshold}")
    y_pred_val = y_pred_val_proba > threshold
    logging.info(classification_report(y_val, y_pred_val))
    logging.info(confusion_matrix(y_val, y_pred_val))

    return

def get_prediction_threshold(model, X, y, beta=2):
    y_pred = model.predict_proba(X)[:, 1]
    precision, recall, thresholds = precision_recall_curve(y, y_pred)
    fscore = (1 + beta ** 2) * precision * recall / (beta ** 2 * precision + recall + 1e-9)
    idx = np.argmax(fscore)
    return thresholds[idx]

def train_rf(df_data, features, target):
    X = df_data[features]
    y = df_data[target]
    X_train, X_val, y_train, y_val = train_test_split(
    X, y,
    test_size=0.2,
    random_state = 42,
    stratify=y
    )
    rf = RandomForestClassifier(class_weight='balanced')
    grid_params = {
    'n_estimators' : [10, 20, 50, 100, 200],
    'max_depth': [None, 3, 5, 7, 9],
    'min_samples_split': [2, 3, 5, 7],
    "max_samples": [0.2, 0.5, 0.7]
    }
    cv = StratifiedKFold(5, shuffle=True, random_state=42)
    grid_search = RandomizedSearchCV(
        estimator=rf,
        param_distributions=grid_params,
        cv=cv,
        n_jobs=-1,
        n_iter=50,
        scoring='average_precision')
    logging.info("training rf classifier")
    grid_search.fit(X_train, y_train)
    best_rf = grid_search.best_estimator_
    logging.info("trained rf classifier")
    threshold = get_prediction_threshold(best_rf, X_val, y_val, 5)
    logging.info(f"trained rf classifier threshold is {threshold}")
    evaluate_model(best_rf, X_train, y_train, X_val, y_val, threshold)
    os.makedirs(os.path.join(BASE_DIR, "output"), exist_ok = True)
    save_artifacts(best_rf, os.path.join(BASE_DIR, "output", "rf_2.joblib"), features, threshold)
    return

def train_lgb(df_data, features, target):
    X = df_data[features]
    y = df_data[target]
    X_train, X_val, y_train, y_val = train_test_split(
    X, y,
    test_size=0.2,
    random_state = 42,
    stratify=y
    )

    lgb_model = lgb.LGBMClassifier(class_weight='balanced', verbose = -1)
    grid_params = {
    'learning_rate' : [0.01, 0.05, 0.1],
    'max_depth': [-1, 3, 5, 7, 9],
    'num_leaves': [30, 100, 200, 500],
    "n_estimators": [50, 100, 150, 200]
    }
    cv = StratifiedKFold(5, shuffle=True, random_state=42)
    grid_search = RandomizedSearchCV(
        n_iter=50,
        estimator=lgb_model,
        param_distributions=grid_params,
        cv=cv,
        n_jobs=-1,
        scoring='average_precision')
    logging.info("training lgb classifier")

    grid_search.fit(X_train, y_train)
    best_lgb = grid_search.best_estimator_
    logging.info("trained lgb classifier")
    threshold = get_prediction_threshold(best_lgb, X_val, y_val, 7)
    logging.info(f"trained lgb classifier threshold is {threshold}")
    evaluate_model(best_lgb, X_train, y_train, X_val, y_val, threshold)
    os.makedirs(os.path.join(BASE_DIR, "output"), exist_ok= True)
    save_artifacts(best_lgb, os.path.join(BASE_DIR, "output", "lgb_2.joblib"), features, threshold)
    return

if __name__ == "__main__":
    transactions_path = os.path.join(BASE_DIR, 'IBM', 'amlWORLD', 'HI-Small_Trans.csv')
    cycled_money_path = os.path.join(BASE_DIR, 'output', 'accounts_cycled_money.csv')
    garg_index_path = os.path.join(BASE_DIR, 'output', 'accounts_garg_index.csv')
    output_path = os.path.join(BASE_DIR, 'output', 'final_data_2.csv')
    df_transactions, df_cycled_money, df_garg_index = load_data(transactions_path, cycled_money_path, garg_index_path)
    df_transactions = clean_transactions_df(df_transactions)
    df_accounts = get_laundering_df(df_transactions)
    graph = populate_graph(df_transactions)
    df_merged = merge_extracted_features(df_accounts, df_cycled_money, df_garg_index)
    df_final = get_features(df_merged, graph)
    save_data(df_final, output_path)
    target = "is_laundering"
    features = set(df_final.columns)
    features.discard(target)
    features.discard("Account_ID")
    features = list(features)
    train_rf(df_final, features, target)
    train_lgb(df_final, features, target)