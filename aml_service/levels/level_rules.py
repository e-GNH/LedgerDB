import json
from collections import defaultdict
import os, sys
from datetime import timedelta
from datetime import datetime

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


with open(os.path.join(BASE_DIR, "config", "thresholds.json"), 'r') as f:
    THRESHOLDS = json.load(f)

## {senderID: [(amount1, timestamp1), (amount2, timestamp2), ,,,,]}
daily_transactions = defaultdict(list)

def clean_daily(sender, timestamp):
    cutoff = timestamp - timedelta(hours=24)
    daily_transactions[sender] = [tx for tx in daily_transactions[sender] if tx[1] > cutoff]
    
def check_transaction(transaction):
    try:
        sender = transaction["sender"]
        amount = transaction["amount"]
        timestamp = transaction.get("timestamp", datetime.now())
        sender_account_type = transaction["sender_account_type"]
        
        if sender_account_type not in THRESHOLDS:
            return False, "Transaction sender account type is wrong"
        
        sender_limits = THRESHOLDS[sender_account_type]
        
        if amount > sender_limits["tx_threshold"]:
            return False, "Transaction amount exceeds transaction limit"
        
        clean_daily(sender, timestamp)
        daily_sum = sum([tx[0] for tx in daily_transactions[sender]])
        
        if amount + daily_sum > sender_limits["daily_limit"]:
            return False, "Daily transaction limit exceeded"
        
        structuring_threshold =  sender_limits["tx_threshold"] * sender_limits["structuring_percentage"]
        if amount >= structuring_threshold:
            count_structuring = 1 + sum([1 for tx in daily_transactions[sender] if tx[0] >= structuring_threshold])
            if count_structuring >= sender_limits["structuring_limit"]:
                return False, "Too many transactions nearing limit"
            
        count_near_transactions = 1 + sum([1 for tx in daily_transactions[sender] if timestamp - tx[1] <= timedelta(minutes=sender_limits["velocity_window_minutes"])])
        if count_near_transactions >= sender_limits["velocity_limit"]:
            return False, "Too many in short time"

        daily_transactions[sender].append([amount, timestamp])

        return True, ""
        
    except Exception as e:
        return False, f"error at level 1 (rules) checks {e}"
        