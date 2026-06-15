from datetime import datetime
import json
import sys
import os

BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__))) # two dirname to get to aml_service path 
    
with open(os.path.join(BASE_DIR, 'config', 'thresholds.json')) as f:
    THRESHOLDS = json.load(f)
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from graph.builder import TransactionsGraph

graph = TransactionsGraph()

def check_transaction(transaction):
    try:
        account_type, amount, sender = transaction["account_type"], transaction["amount"], transaction["sender"]
        receiver = transaction["receiver"]
        timestamp = transaction.get("timestamp", datetime.now())

        if account_type not in THRESHOLDS:
            return False, "invalid account type for transaction"
        
        graph.add_transaction(sender, receiver, amount, timestamp)
        limits = THRESHOLDS[account_type]
        if limits["max_fan_out"] < graph.get_outdegree(sender):
            return False, "Fan out limit exceeded for sender"
        
        if limits["max_fan_in"] < graph.get_indegree(receiver):
            return False, "Fan in limit exceeded for receiver"
        
        sender_output_money = graph.get_output_money(sender)
        if sender_output_money >= limits["output_money_amount_check_cycles"]:
            cycled_money = graph.get_money_cycled(sender)
            if cycled_money >= limits["output_money_amount_check_cycles"] * limits["cycle_amount_percentage"]:
                return False, f"Output money cycled back to sender exceeds threshold, {cycled_money} cycled back out of {sender_output_money} total output money"
        
        receiver_output_money = graph.get_output_money(receiver)
        if receiver_output_money >= limits["output_money_amount_check_cycles"]:
            cycled_money = graph.fast_get_money_cycled(receiver)
            if cycled_money >= limits["output_money_amount_check_cycles"] * limits["cycle_amount_percentage"]:
                return False, f"Output money cycled back to receiver exceeds threshold, {cycled_money} cycled back out of {receiver_output_money} total output money"
        
        return True, ""
    except Exception as e:
        return False, f"error at graph level: {e}"

def add_transaction(transaction):
    try:
        account_type, amount, sender = transaction["account_type"], transaction["amount"], transaction["sender"]
        receiver = transaction["receiver"]
        timestamp = transaction.get("timestamp", datetime.now())

        if account_type not in THRESHOLDS:
            return False, "invalid account type for transaction"
        
        graph.add_transaction(sender, receiver, amount, timestamp)
        return True, ""
    except Exception as e:
        return False, f"error at graph level: {e}"
    
def check_account(account, account_type):
    limits = THRESHOLDS[account_type]
    if limits["max_fan_out"] < graph.get_outdegree(account):
            return False, "Fan out limit exceeded for account"
        
    if limits["max_fan_in"] < graph.get_indegree(account):
        return False, "Fan in limit exceeded for account"
    
    account_output_money = graph.get_output_money(account)
    if account_output_money >= limits["output_money_amount_check_cycles"]:
        cycled_money = graph.fast_get_money_cycled(account)
        if cycled_money >= limits["output_money_amount_check_cycles"] * limits["cycle_amount_percentage"]:
            return False, f"Output money cycled back to account exceeds threshold, {cycled_money} cycled back out of {account_output_money} total output money"
    return True, ""

def get_graph():
    return graph