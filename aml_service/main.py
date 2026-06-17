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

from loguru import logger

class Transaction(BaseModel):
    sender: str
    receiver: str
    amount: float
    timestamp: datetime
    sender_account_type: str
    receiver_account_type: str
    
app = FastAPI()

MLLevel_instance = None
account_types = dict()

@app.post("/check_transaction")
def check_transaction(transaction_data: Transaction):
    try:
        transaction = transaction_data.model_dump()
        transaction["account_type"] = transaction["sender_account_type"]
        account_types[transaction["sender"]] = transaction["sender_account_type"]
        account_types[transaction["receiver"]] = transaction["receiver_account_type"]
        logger.info(f"Received transaction: {transaction}")
        if transaction["receiver"] == transaction["sender"]:
            return {
                "status": "rejected",
                "reason": "Sender and receiver cannot be the same"
            }
        ok, reason = level_1_check_transaction(transaction)

        if ok:
            level_2_add_transaction(transaction)
            
        logger.info(f"Return result from level 1 check: {(ok, reason)}")
        
        return {
            "status": "approved" if ok else "rejected",
            "reason": reason
        }
    except Exception as e:
        logger.error(f"Error processing transaction: {e}")
        return {
            "status": "INTERNAL_ERROR",
            "reason": str(e)
        }

@app.post("/check_accounts")
def check_accounts():
    try:
        global MLLevel_instance
        
        graph = level_2_get_graph()
        
        current_time = datetime.now(ZoneInfo("Africa/Cairo"))
        previous_week_time = current_time - timedelta(days=7)
        
        graph.clean_edges(cutoff_time=previous_week_time)
        
        level_2_results = {}
        level_3_results = {}
        if len(graph.get_accounts()) == 0:
            logger.warning("Graph is empty, skipping account checks")
            return {
                "status": "success",
                "reason": "Graph is empty, no accounts to check",
                "level_2": [],
                "level_3": []   
            }
        for account in graph.get_accounts():
            if account not in account_types:
                logger.warning(f"Account {account} not found in account types mapping, skipping account level checks, defaulting to individual")
                account_type = "individual"
            else:
                account_type = account_types[account]
            result, reason = level_2_check_account(account, account_type)
            level_2_results[account] = {"result": result, "reason": reason}
        
        current_accounts = set(graph.get_accounts())
        to_delete_types_cache = set()
        for account in account_types.keys():
            if account not in current_accounts:
                to_delete_types_cache.add(account)
        for account in to_delete_types_cache:
            del account_types[account]
        
        if MLLevel_instance is None:
            MLLevel_instance = MLLevel(graph)
        else:
            MLLevel_instance.update_graph(graph)
        level_3_results = MLLevel_instance.predict_all()

        level_2_response = []
        level_3_response = []
        ## Only include accounts that failed level 2 checks in the response
        for account in level_2_results:
            result = level_2_results[account]
            if not result["result"]:
                level_2_response.append({
                    "account": account,
                    "reason": result["reason"]
                })
        for account in level_3_results:
            result = level_3_results[account]
            if result[0]:  ## result[0] is the boolean indicating if the account is flagged in level 3 check
                level_3_response.append({
                    "account": account,
                    "score": result[1]  ## result[1] is the score from the ML model
                })

        return {
            "status": "success",
            "reason": "",
            "level_2": level_2_response,
            "level_3": level_3_response
        }
    except Exception as e:
        logger.error(f"Error processing account checks: {e}")
        return {
            "status": "INTERNAL_ERROR",
            "reason": str(e),
            "level_2": [],
            "level_3": []
        }
