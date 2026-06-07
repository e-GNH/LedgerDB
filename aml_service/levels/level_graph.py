import sys
import os
sys.path.append(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from graph.builder import TransactionsGraph

gf = TransactionsGraph()

gf.add_transaction('A', 'B', 1000, 1)
gf.add_transaction('B', 'C', 800, 2)
gf.add_transaction('B', 'C', 100, 2)
gf.add_transaction('C', 'A', 100, 3)
gf.add_transaction('C', 'A', 100, 3)
print(gf.get_money_cycled('A')) ## should return 1000