## Cycle Detection (`fast_get_money_cycled`)

**Objective:** Detect money laundering cycles within a 5-million edge financial network, constrained by maximum hop depth (`LOOP_CUTOFF`) and strictly chronological transaction flow.

### Function Versions

**V1: Full Path Materialization**
* **Approach:** Utilized `nx.all_simple_edge_paths` to discover all cyclic paths, stored them in memory, and calculated remaining capacities sequentially.
* **Limitation:** Path explosion ($O(V!)$). A depth limit of 4 in dense financial subgraphs generates millions of paths per account. Very Slow since has no type of pruning and memory heavy.

**V2: Custom DFS Materialization**
* **Approach:** Implemented a custom Depth-First Search (DFS) for finer control over node traversal and visited states.
* **Limitation:** The DFS accumulated successful routes in a memory array (`paths.append`) prior to resolving edge capacities, causing big memory footprint.

**V3: Dynamic Flow Routing (Current Implementation)**
* **Approach:** Eliminates path storage entirely. Implements a time-aware, capacity routing directly within the DFS traversal stack.
* **Core Engineering Mechanics:**
  1. **$O(Depth)$ Memory Footprint:** Utilizes a standard array stack (`current_path.append`/`.pop`). The algorithm never holds more edges in memory than the defined `LOOP_CUTOFF`.
  2. **Strict Chronology:** Both root out-edges and internal DFS branches are sorted by timestamp. This guarantees that future transactions cannot drain network capacity required by old transactions.
  3. **Sibling Branch Isolation:** Dynamically calculates available node capacity during traversal (`current_available = path_bottleneck - total_returned_from_children`). This prevents sibling branches from double-counting the upstream money flow.
  4. **Early Stopping (CPU Optimization):** Prunes traversal branches instantly if `edge_path_bottleneck <= 0`. Deeply saves time.