import json
from datetime import datetime, timedelta
from collections import defaultdict
import os

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__))) # two dirname to get to aml_service path 

with open(os.path.join(BASE_DIR, 'config', 'thresholds.json')) as f:
    THRESHOLDS = json.load(f)

# { sender_id: [{ amount, timestamp }, ...] }
daily_window = defaultdict(list)

def clean_sender_window(sender, timestamp):
    cutoff = timestamp - timedelta(hours=24)
    daily_window[sender] = [tx for tx in daily_window[sender] if tx[1] > cutoff]

def check_transaction(transaction):
    try:
        account_type, amount, sender = transaction["account_type"], transaction["amount"], transaction["sender"]
        if account_type not in THRESHOLDS:
            return False, "invalid account type for transaction"
        
        limits = THRESHOLDS[account_type]
        
        if amount > limits["tx_threshold"]:
            return False, "Transaction amount exceeds transaction limit"
      
        timestamp = transaction.get("timestamp", datetime.now())
        clean_sender_window(sender, timestamp)
        total_tx_day = sum([tx[0] for tx in daily_window[sender]])
        
        if(total_tx_day + amount > limits["daily_limit"]):
            return False, "Daily Transaction Amount exceeded"
        
        ## structuring
        structuring_check_amount = limits["structuring_percentage"] * limits["tx_threshold"]
        if amount >= structuring_check_amount:
            count = sum([1 for tx in daily_window[sender] if tx[0] >= structuring_check_amount])
            if count + 1 >= limits["structuring_limit"]:
                return False, "Too many transactions nearing limit"
        ## rapid transactions
        count_near_interval = sum([1 for tx in daily_window[sender] if timestamp - tx[1] <= timedelta(minutes=limits["velocity_window_minutes"])])
        if count_near_interval + 1 >= limits["velocity_limit"]:
            return False, "Too many transactions in short time"
        
        daily_window[sender].append([amount, timestamp])
        
        return True, ""
    except Exception as e:
        return False, f"invalid transaction: {e}"