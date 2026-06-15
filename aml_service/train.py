import logging
from datetime import datetime
import json
import sys
import os
import pandas as pd
import lightgbm as lgb

BASE_DIR = os.path.dirname((os.path.abspath(__file__))) # two dirname to get to aml_service path 
import joblib

with open(os.path.join(BASE_DIR, 'config', 'thresholds.json')) as f:
    THRESHOLDS = json.load(f)
sys.path.append(BASE_DIR)
from sklearn.metrics import classification_report, confusion_matrix, precision_recall_curve
from graph.builder import TransactionsGraph
from sklearn.model_selection import train_test_split
from sklearn.ensemble import RandomForestClassifier
from sklearn.model_selection import GridSearchCV, StratifiedKFold
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - [%(levelname)s] - %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S'
)

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
    logging.info(f"Reading data from {file_path}...")

    df = pd.read_csv(file_path)
    logging.info("Data reading completed.")
    return df

def load_data(transactions_path, cycled_money_path, garg_index_path):
    logging.info("Starting data extraction...")
    df_transactions = read_csv(transactions_path)
    df_cycled_money = read_csv(cycled_money_path)
    df_garg_index = read_csv(garg_index_path)
    logging.info("Data extraction completed.")
    return df_transactions, df_cycled_money, df_garg_index

def clean_transactions_df(df_transactions):
    df_transactions["From_Account"] = (
    df_transactions["From Bank"].astype("string")
    .str.cat(df_transactions["Account"].astype("string"), sep="_")
    )
    df_transactions["To_Account"] = (
        df_transactions["To Bank"].astype("string")
        .str.cat(df_transactions["Account.1"].astype("string"), sep="_")
    )
    df_transactions.drop(columns=["From Bank", "Account", "To Bank", "Account.1"], inplace=True)
    
    
    df_transactions["Amount_Paid_EGP"] = df_transactions.apply(
    lambda row: row["Amount Paid"] * CURRENCY_EXCHANGE[row["Payment Currency"]],
    axis=1
)
    df_transactions["Timestamp"] = pd.to_datetime(df_transactions["Timestamp"])

    return df_transactions

def get_laundering_df(df_transactions):
    all_accounts = list(set(df_transactions["From_Account"]).union(set(df_transactions["To_Account"])))
    laundering_transactions = df_transactions[df_transactions["Is Laundering"] == 1]
    laundering_accounts = set(laundering_transactions["From_Account"]).union(set(laundering_transactions["To_Account"]))
    df_accounts = pd.DataFrame({
    "Account_ID": all_accounts,
    "is_laundering": [1 if acc in laundering_accounts else 0 for acc in all_accounts]
    })
    return df_accounts

def populate_graph(df_transactions):
    graph = TransactionsGraph()
    for index, row in df_transactions.iterrows():
        if index % 100000 == 0:
            logging.info(f"Processing transaction {index} / {len(df_transactions)}")
        graph.add_transaction(from_account=row["From_Account"], to_account=row["To_Account"], amount=row["Amount_Paid_EGP"], timestamp=row["Timestamp"])
    return graph

def merge_extracted_features(df_accounts, df_cycled_money, df_garg_index):
    df_data = pd.merge(df_accounts, df_cycled_money, how="left", left_on="Account_ID", right_on="Account_ID")
    df_data = pd.merge(df_data, df_garg_index[["Account_ID", "garg_index"]], how="left", left_on="Account_ID", right_on="Account_ID")
    return df_data

def save_data(df_data, output_path):
    df_data.to_csv(output_path, index=False)
    logging.info(f"Data saved to {output_path}")

def get_features(df_data, graph):
    df_data["Input_Output_Ratio"] = df_data["Input_Money"] / (df_data["Output_Money"] + 1e-6)
    df_data["Cycled_Input_Ratio"] = df_data["Cycled_Money"] / (df_data["Input_Money"] + 1e-6)
    df_data["Cycled_Output_Ratio"] = df_data["Cycled_Money"] / (df_data["Output_Money"] + 1e-6)
    df_data["indegree"] = df_data["Account_ID"].apply(lambda x: graph.get_indegree(x))
    df_data["outdegree"] = df_data["Account_ID"].apply(lambda x: graph.get_outdegree(x))
    df_data["Transactions_Out"] = df_data["Account_ID"].apply(lambda x: graph.graph.out_degree(x) if x in graph.graph else 0) ## out_degree of graph is number of edges
    df_data["Transactions_In"] = df_data["Account_ID"].apply(lambda x: graph.graph.in_degree(x) if x in graph.graph else 0) ## in_degree of graph is number of edges
    df_data["Avg_TX_in"] = df_data["Input_Money"] / (df_data["Transactions_In"] + 1e-6)
    df_data["Avg_TX_out"] = df_data["Output_Money"] / (df_data["Transactions_Out"] + 1e-6)
    df_data["Transactions_Per_Input"] = df_data["Transactions_In"] / (df_data["indegree"] + 1e-6)
    df_data["Transactions_Per_Output"] = df_data["Transactions_Out"] / (df_data["outdegree"] + 1e-6)
    
    return df_data

def save_artifacts(model, output_path, features, threshold):
    artifacts = {
        'model': model,
        'features': features,
        'threshold': threshold
    }
    joblib.dump(artifacts, output_path)
    logging.info(f"Production artifacts saved to {output_path}")

def evaluate_model(model, X_train, y_train, X_val, y_val, threshold=0.3):
    logging.info("Performance On Train Set:")
    y_pred_train_proba = model.predict_proba(X_train)[:, 1]
    y_pred_train = (y_pred_train_proba > threshold).astype(int)
    logging.info(classification_report(y_train, y_pred_train))
    logging.info(confusion_matrix(y_train, y_pred_train))

    logging.info("Performance on Validation Set:")
    y_pred_proba = model.predict_proba(X_val)[:, 1]
    y_pred = (y_pred_proba > threshold).astype(int)
    logging.info(classification_report(y_val, y_pred))
    logging.info(confusion_matrix(y_val, y_pred))
    
def get_prediction_threshold(model, X, y, beta=2):
        y_proba = model.predict_proba(X)[:, 1]
        precision, recall, thresholds = precision_recall_curve(y, y_proba)
        f_beta_scores = (1 + beta**2) * (precision * recall) / (beta**2 * precision + recall + 1e-6)
        best_idx = f_beta_scores.argmax()
        return thresholds[best_idx], f_beta_scores[best_idx]

def train_rf(df_data, features, target):
    X, y = df_data[features], df_data[target]
    X_train, X_val, y_train, y_val = train_test_split(
        X, y, 
        test_size=0.2, 
        random_state=42, 
        stratify=y  # Maintains the ratio in both splits
    )

    rf = RandomForestClassifier(random_state=42, class_weight='balanced')
    cv = StratifiedKFold(n_splits=5, shuffle=True, random_state=42)
    param_grid = {
        'n_estimators': [100, 200],
        'max_depth': [5, 10, 15],
        'min_samples_leaf': [10, 50, 100], 
        'max_samples': [0.5, 0.8]
    }
    ## Scoring Changed from F1 score to average precision which calculates area under precision-recall curve
    grid_search = GridSearchCV(
        estimator=rf, 
        param_grid=param_grid, 
        cv=cv, 
        scoring='average_precision',
        n_jobs=-1
    )
    grid_search.fit(X_train, y_train)
    best_model = grid_search.best_estimator_
    logging.info("Best Hyperparameters for random forest:", grid_search.best_params_)
    # Evaluation on test set
    prediction_threshold, _ = get_prediction_threshold(best_model,X_val, y_val, beta=5)
    logging.info(f"Optimal prediction threshold determined for rf model: {prediction_threshold}")
    evaluate_model(best_model, X_train, y_train, X_val, y_val, threshold=prediction_threshold)
    save_artifacts(best_model, os.path.join(BASE_DIR, 'output', 'rf_model.joblib'), features, prediction_threshold)
    return best_model

def train_lgb(df_data, features, target):
    X, y = df_data[features], df_data[target]
    X_train, X_val, y_train, y_val = train_test_split(
        X, y, 
        test_size=0.2, 
        random_state=42, 
        stratify=y  # Maintains the ratio in both splits
    )
    ratio = len(y_train[y_train==0]) / len(y_train[y_train==1])

    lgb_model = lgb.LGBMClassifier(
        scale_pos_weight=ratio,
        random_state=42,
        n_jobs=-1,
        verbose=-1
    )

    lgb_model.fit(X_train, y_train)
    param_grid = {
        'n_estimators': [100, 300],        
        'learning_rate': [0.05, 0.1],    
        'num_leaves': [31, 50],       
        'max_depth': [5, 10],             
        'min_child_samples': [20, 50],   
        'subsample': [0.8]               
    }
    cv = StratifiedKFold(n_splits=5, shuffle=True, random_state=42)

    grid_search_lgb = GridSearchCV(
        estimator=lgb_model, 
        param_grid=param_grid, 
        cv=cv, 
        scoring='average_precision', 
        n_jobs=-1
    )

    logging.info("Starting LightGBM Grid Search...")
    grid_search_lgb.fit(X_train, y_train)
    logging.info("Best Parameters:", grid_search_lgb.best_params_)
    best_model_lgb = grid_search_lgb.best_estimator_
    prediction_threshold, _ = get_prediction_threshold(best_model_lgb,X_val, y_val, beta=7)
    logging.info(f"Optimal prediction threshold determined for lgb model: {prediction_threshold}")
    evaluate_model(best_model_lgb, X_train, y_train, X_val, y_val, threshold=prediction_threshold)
    save_artifacts(best_model_lgb, os.path.join(BASE_DIR, 'output', 'lgb_model.joblib'), features, prediction_threshold)
    return best_model_lgb

if __name__ == "__main__":
    
    transactions_path = os.path.join(BASE_DIR, 'IBM', 'amlWORLD', 'HI-Small_Trans.csv')
    cycled_money_path = os.path.join(BASE_DIR, 'output', 'accounts_cycled_money.csv')
    garg_index_path = os.path.join(BASE_DIR, 'output', 'accounts_garg_index.csv')
    output_path = os.path.join(BASE_DIR, 'output', 'final_dataset.csv')

    df_transactions, df_cycled_money, df_garg_index = load_data(transactions_path, cycled_money_path, garg_index_path) 
    
    df_transactions = clean_transactions_df(df_transactions)
    df_accounts = get_laundering_df(df_transactions)
    graph = populate_graph(df_transactions)
    df_data = merge_extracted_features(df_accounts, df_cycled_money, df_garg_index)
    df_data = get_features(df_data, graph)
    
    save_data(df_data, output_path)
    
    features = list(df_data.columns.difference(['Account_ID', 'is_laundering']))
    target = 'is_laundering'
    
    train_rf(df_data, features, target)
    train_lgb(df_data, features, target)
    
    