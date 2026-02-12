```mermaid
flowchart TD
    %% 1. Clients send requests to Proxies
    subgraph Clients [Clients]
        U1(User A)
        U2(User B)
        Un(User N...)
    end

    subgraph Proxies [Execution Layer]
        LP1[Ledger Proxy 1]
        LP2[Ledger Proxy N...]
        WS[(World State)]
    end

    %% 2. Proxies Execute & Write Payload to Storage
    subgraph Storage [Storage Layer]
        HDFS[(HDFS Storage Kernel)]
    end

    %% 3. Sequencing & Indexing
    subgraph Core [Ordering Layer]
        LM{{Ledger Master\nGlobal Sequencer}}
        LS1[Ledger Server 1]
        LS2[Ledger Server N...]
    end

    %% Connections
    U1 & U2 & Un -->|1. Send Request| LP1 & LP2
    LP1 & LP2 -->|2. Write Payload RPC| HDFS
    LP1 & LP2 -.->|3. Update State| WS
    
    LP1 & LP2 -->|4. Submit for Ordering| LM
    LM -->|5. Assign Batch| LS1 & LS2
    
    LS1 & LS2 -->|6. Commit Metadata| HDFS
    LS1 & LS2 -->|7. Build Indexes| LS1 & LS2
    
    %% Return Path
    LS1 & LS2 -.->|8. Return Receipt| U1 & U2 & Un

    %% Styling
    classDef storage fill:#f9f,stroke:#333,stroke-width:2px;
    class HDFS,WS storage;
    classDef master fill:#ff9,stroke:#333,stroke-width:2px;
    class LM master;
