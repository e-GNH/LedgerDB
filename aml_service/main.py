from fastapi import FastAPI
import os, sys
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.append(BASE_DIR)
from levels.level_rules import check_transaction as level_1_check_transaction
from levels.level_graph import check_transaction as level_2_check_transaction
from levels.level_graph import check_account as level_2_check_account
from levels.level_graph import add_transaction as level_2_add_transaction
from levels.level_graph import get_graph as level_2_get_graph
from levels.level_ml import MLLevel

from datetime import datetime, timedelta
from pydantic import BaseModel
from zoneinfo import ZoneInfo

import logging

BIPARTITE_GRAPH_THRESHOLD = 5

class Transaction(BaseModel):
    sender: str
    receiver: str
    amount: float
    timestamp: datetime
    sender_account_type: str
    receiver_account_type: str
    
app = FastAPI()

ml_level_instance = None
account_types = dict()

@app.post("/check_transaction")
def check_transaction(transaction_data: Transaction):
    try:
        transaction = transaction_data.model_dump()
        transaction["account_type"] = transaction["sender_account_type"]
        account_types[transaction["sender"]] = transaction["sender_account_type"]
        account_types[transaction["receiver"]] = transaction["receiver_account_type"]
        
        if (transaction["sender"] == transaction["receiver"]):
            return {
                "status": "rejected",
                "reason": "sender and receiver can't be the same",
            }
        
        ok, reason = level_1_check_transaction(transaction)
        logging.info(f"results checking transaction {ok}, {reason}")
        if ok:
            level_2_add_transaction(transaction)
            return {
                "status": "approved",
                "reason": reason,
            }
        
        return {
                "status": "rejected",
                "reason": reason,
        }
    except Exception as e:
        logging.warning(f"error checking transaction {e}")
        return {
                "status": "INTERNAL_ERROR",
                "reason": str(e),
        }

@app.post("/check_accounts")
def check_accounts():
    try:
        global ml_level_instance
        global account_types
        graph = level_2_get_graph()
        
        current_time = datetime.now(ZoneInfo("Africa/Cairo"))
        cutoff_time = current_time - timedelta(days=7)
        graph.clean_edges(cutoff_time)
        if len(graph.get_accounts()) == 0:
            logging.warning(f"empty graph for check accounts")
            return {
            "status": "success",
            "reason": "graph is empty",
            "level_2": [],
            "level_3": []
            }
            
        curr_graph_accounts = set(graph.get_accounts())
        curr_accounts_types_cached = set(account_types.keys())
        
        for acc in curr_accounts_types_cached:
            if acc not in curr_graph_accounts:
                del account_types[acc]
                
        if ml_level_instance is None:
            ml_level_instance = MLLevel(graph)
        else:
            ml_level_instance.update_graph(graph)
        
        level_2_results = {}
        level_3_results = {}

        for account in graph.get_accounts():
            if account not in account_types:
                logging.warning(f"account {account} not in account_types defaulting to PERSON")
                account_types[account] = "PERSON"
            result, reason = level_2_check_account(account, account_types[account])
            if not result:
                ## account check failed
                level_2_results[account] = {
                    "reason": reason,
                }
        
        level_3_results = ml_level_instance.predict_all()
        level_3_flagged_accounts = []
        for account in level_3_results:
            if level_3_results[account][0]:
                level_3_flagged_accounts.append({
                    "account": account,
                    "score": level_3_results[account][1]
                })
                
        communities =ml_level_instance.get_communities()
        for community in communities:
            is_bipartite = graph.check_bipartite_subgraph(community, BIPARTITE_GRAPH_THRESHOLD)
            if is_bipartite:
                for account in community:
                    if account not in level_2_results:
                        level_2_results[account] = {
                            "reason":  "account is a part of bipartite graph"
                            }
        
        level_2_flagged_accounts = list()
        for account in level_2_results:
            level_2_flagged_accounts.append({ 
                                        "account":account,
                                        "reason": level_2_results[account]["reason"]
                                        })
        return {
            "status": "success",
            "reason": "",
            "level_2": level_2_flagged_accounts,
            "level_3": level_3_flagged_accounts
        }
            
    except Exception as e:
        
        logging.warning(f"error checking accounts {e}")
        return {
                "status": "INTERNAL_ERROR",
                "reason": str(e),
                "level_2": [],
                "level_3": []
        }