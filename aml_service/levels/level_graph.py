
import json
from collections import defaultdict
import os, sys
from datetime import timedelta
from datetime import datetime

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


with open(os.path.join(BASE_DIR, "config", "thresholds.json"), 'r') as f:
    THRESHOLDS = json.load(f)
    
sys.path.append(BASE_DIR)

from graph.builder import TransactionsGraph

graph = TransactionsGraph()

def get_graph():
    return graph

def check_transaction(transaction):
    try:
        sender = transaction["sender"]
        amount = transaction["amount"]
        timestamp = transaction.get("timestamp", datetime.now())
        sender_account_type = transaction["sender_account_type"]
        if sender_account_type not in THRESHOLDS:
            return False, "Wrong Account type"

        sender_limits = THRESHOLDS[sender_account_type]
        receiver = transaction["receiver"]

        
        graph.add_transaction(sender, receiver, amount, timestamp)
        
        sender_fan_out = graph.get_outdegree(sender)
        if sender_fan_out > sender_limits["max_fan_out"]:
            return False, "Sender fan out limit exceeded, sent to many accounts"
        
        
        sender_output_money_cycling_check = sender_limits["output_money_amount_check_cycles"]
        sender_cycled_money_threshold = sender_output_money_cycling_check * sender_limits["cycle_amount_percentage"]
        if graph.get_output_money(sender) >= sender_output_money_cycling_check:
            if graph.get_input_money(sender) >= sender_cycled_money_threshold:
                cycled = graph.fast_get_money_cycled(sender)
                if cycled >= sender_cycled_money_threshold:
                    return False, f"sender cycled money back more than limit, {cycled} money cycled"        
        
        if "receiver_account_type" in transaction and transaction["receiver_account_type"] in THRESHOLDS:
            receiver_account_type = transaction["receiver_account_type"]
            receiver_limits = THRESHOLDS[receiver_account_type]
            
            receiver_fan_in = graph.get_indegree(receiver)
            if receiver_fan_in > receiver_limits["max_fan_in"]:
                return False, "receiver fan in limit exceeded, received from many accounts"        
            
            receiver_output_money_cycling_check = receiver_limits["output_money_amount_check_cycles"]
            receiver_cycled_money_threshold = receiver_output_money_cycling_check * receiver_limits["cycle_amount_percentage"]
            if graph.get_output_money(receiver) >= receiver_output_money_cycling_check:
                if graph.get_input_money(receiver) >= receiver_cycled_money_threshold:
                    cycled = graph.fast_get_money_cycled(receiver)
                    if cycled >= receiver_cycled_money_threshold:
                        return False, f"receiver cycled money back more than limit, {cycled} money cycled"    
        
        return True, ""
    except Exception as e:
        return False, f"error at level 2 (graph) checks {e}"

def add_transaction(transaction):
    try:
        from_account = transaction["sender"]
        to_account = transaction["receiver"]
        amount = transaction["amount"]
        timestamp = transaction.get("timestamp", datetime.now())
        if transaction["sender_account_type"] not in THRESHOLDS:
            return False, "Wrong Account type"
        graph.add_transaction(from_account, to_account, amount, timestamp)
        return True, ""
    except Exception as e:
        return False, f"error at level 2 (graph) checks {e}"
    
def check_account(account, account_type):
    if account_type not in THRESHOLDS:
        return False, f"Wrong Account type"
    
    limits = THRESHOLDS[account_type]
    
    gather_accounts, scatter_gather_max_val = graph.check_scatter_gather(account, levels=4, threshold_scatter=limits["scatter_accounts_threshold"])
    if scatter_gather_max_val >= limits["gather_accounts_threshold"]:
        return False, f"Sender suspected to participate in scatter gather account to {', '.join(gather_accounts)} with value {scatter_gather_max_val}"

    
    account_fan_out = graph.get_outdegree(account)
    fan_in = graph.get_indegree(account)
    if account_fan_out > limits["max_fan_out"] and fan_in > limits["max_fan_in"]:
            money_in = graph.get_input_money(account)
            money_out = graph.get_output_money(account)
            if money_out / money_in > limits["gather_scatter_money_ratio"]:
                return False, f"Sender fan out and fan in exceeded (Gather-Scatter), money out {money_out}, money in {money_in}"

    if account_fan_out > limits["max_fan_out"]:
        return False, "Sender fan out limit exceeded, sent to many accounts"
    
    
    if fan_in > limits["max_fan_in"]:
        return False, "account fan in limit exceeded, received from many accounts"        
    
    output_money_cycling_check = limits["output_money_amount_check_cycles"]
    cycled_money_threshold = output_money_cycling_check * limits["cycle_amount_percentage"]
    if graph.get_output_money(account) >= output_money_cycling_check:
        if graph.get_input_money(account) >= cycled_money_threshold:
            cycled = graph.fast_get_money_cycled(account)
            if cycled >= cycled_money_threshold:
                return False, f"account cycled money back more than limit, {cycled} money cycled"        
    
    return True, ""

if __name__ == "__main__":
    tx1 = {
        "sender": "A",
        "receiver": "B",
        "amount": 1,
        "timestamp": 1,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    tx1 = {
        "sender": "A",
        "receiver": "C",
        "amount": 1,
        "timestamp": 1,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    tx1 = {
        "sender": "A",
        "receiver": "D",
        "amount": 1,
        "timestamp": 1,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    tx1 = {
        "sender": "B",
        "receiver": "E",
        "amount": 1,
        "timestamp": 1,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    tx1 = {
        "sender": "C",
        "receiver": "E",
        "amount": 1,
        "timestamp": 1,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    tx1 = {
        "sender": "D",
        "receiver": "E",
        "amount": 1,
        "timestamp": 0,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    tx1 = {
        "sender": "E",
        "receiver": "F",
        "amount": 1,
        "timestamp": 1,
        "sender_account_type" :"POS",
        "account_type" :"POS"
    }
    add_transaction(tx1)
    print(check_account("A", "POS"))