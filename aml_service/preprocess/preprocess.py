from datetime import datetime
import json
import sys
import os
import concurrent.futures
import time
from tqdm import tqdm
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__))) # two dirname to get to aml_service path 
    
with open(os.path.join(BASE_DIR, 'config', 'thresholds.json')) as f:
    THRESHOLDS = json.load(f)
sys.path.append(BASE_DIR)

from graph.builder import TransactionsGraph
csv_path = "/Users/zeyaddaowd/Desktop/GP/LedgerDB/aml_service/IBM/amlWORLD/HI-Small_Trans.csv"
import pandas as pd
df = pd.read_csv(csv_path)
df["From_Account"] = (
    df["From Bank"].astype("string")
    .str.cat(df["Account"].astype("string"), sep="_")
)
df["To_Account"] = (
    df["To Bank"].astype("string")
    .str.cat(df["Account.1"].astype("string"), sep="_")
)
df.drop(columns=["From Bank", "Account", "To Bank", "Account.1"], inplace=True)

currency_exchange = {
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
df["Amount_Paid_EGP"] = df.apply(
    lambda row: row["Amount Paid"] * currency_exchange[row["Payment Currency"]],
    axis=1
)
graph = TransactionsGraph()

for index, row in df.iterrows():
    if index % 100000 == 0:
        print(f"Processing transaction {index} / {len(df)}")
    graph.add_transaction(from_account=row["From_Account"], to_account=row["To_Account"], amount=row["Amount_Paid_EGP"], timestamp=row["Timestamp"])



def compute_cycled_money(account):
    return account, graph.fast_get_money_cycled(account)

if __name__ == '__main__':
    start_time = time.time()
    total = len(graph.graph.nodes)
    cycled_money = {}
    all_accounts = list(graph.graph.nodes)
    with concurrent.futures.ProcessPoolExecutor(max_workers=4) as executor:
        
        futures = [executor.submit(compute_cycled_money, acc) for acc in all_accounts]
        for future in tqdm(concurrent.futures.as_completed(futures), total=total):
            account, cycled_amt = future.result()
            cycled_money[account] = cycled_amt
    print("Total time taken to compute cycled money for all accounts:", time.time() - start_time, "seconds")
    ## dump cycled_money to json
    with open(os.path.join(BASE_DIR, 'output', 'cycled_money.json'), 'w') as f:
        json.dump(cycled_money, f)
    
    accounts_df = pd.DataFrame({"Account_ID": all_accounts})
    accounts_df["Cycled_Money"] = accounts_df["Account_ID"].map(cycled_money)
    accounts_df["Input_Money"] = accounts_df["Account_ID"].apply(lambda x: graph.get_input_money(x))
    accounts_df["Output_Money"] = accounts_df["Account_ID"].apply(lambda x: graph.get_output_money(x))
    accounts_df.to_csv(('accounts_cycled_money.csv'), index=False)
