from datetime import datetime
import json
import sys
import os
import concurrent.futures
import multiprocessing as mp
import time
from tqdm import tqdm
import pandas as pd



BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__))) # two dirname to get to aml_service path 

with open(os.path.join(BASE_DIR, 'config', 'thresholds.json')) as f:
    THRESHOLDS = json.load(f)
sys.path.append(BASE_DIR)
from graph.garg_index import GargIndex

csv_path = "/Users/zeyaddaowd/Desktop/GP/LedgerDB/aml_service/IBM/amlWORLD/HI-Small_Trans.csv"

graph = None
if __name__ == '__main__':
    mp.set_start_method('fork')
    df = pd.read_csv(csv_path)
    df["From_Account"] = (
        df["From Bank"].astype("string")
        .str.cat(df["Account"].astype("string"), sep="_")
    )
    df["To_Account"] = (
        df["To Bank"].astype("string")
        .str.cat(df["Account.1"].astype("string"), sep="_")
    )
    df.drop(columns=["From Bank", "Account", "To Bank", "Account.1", "Timestamp"], inplace=True)
    
    print("Initializing Graph...")
    graph = GargIndex(df, key_from="From_Account", key_to="To_Account")
    print("Pre-calculating Louvain communities...")
    graph._get_community() 
    
    print("Launching GARG scoring...")
    start_time = time.time()
    garg_index = {}
    all_accounts = list(set(df["From_Account"]).union(set(df["To_Account"])))
    total = len(all_accounts)
    for account in tqdm(all_accounts, total=total):
        garg_index[account] = graph.get_score(account)
    print("Total time taken to compute garg index for all accounts:", time.time() - start_time, "seconds")
    ## dump garg_index to json
    os.makedirs(os.path.join(BASE_DIR, 'output'), exist_ok=True)
    with open(os.path.join(BASE_DIR, 'output', 'garg_index.json'), 'w') as f:
        json.dump(garg_index, f)

    accounts_df = pd.DataFrame({"Account_ID": all_accounts})
    accounts_df["garg_index"] = accounts_df["Account_ID"].map(garg_index)
    accounts_df.to_csv(os.path.join(BASE_DIR, 'output', 'accounts_garg_index.csv'), index=False)
    print("Garg index for accounts saved to:", os.path.join(BASE_DIR, 'output', 'accounts_garg_index.csv'))
