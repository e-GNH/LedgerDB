flowchart TD
    %% 1. The Many Users
    subgraph Users [Users]
        U1(User A)
        U2(User B)
        Un(User N...)
    end

    %% 2. The Single Communication Entry Point (Representing Many Proxies)
    subgraph CommLayer [Communication Layer]
        Proxy[Communication Server\n(Ledger Proxies)]
        WS[(World State)]
    end

    %% 3. The Shared Storage
    subgraph Storage [Storage Layer]
        HDFS[(HDFS Storage Kernel)]
    end

    %% 4. The Core Logic (Master + Server Blocks)
    subgraph Core [Core System]
        LM{{Ledger Master\nGlobal Sequencer}}
        
        %% The Servers are just blocks in a cluster
        subgraph ServerCluster [Ledger Servers]
            direction LR
            LS1[Server 1]
            LS2[Server 2]
            LS3[Server 3]
        end
    end

    %% ---- FLOW CONNECTIONS ----

    %% Users -> Communication
    U1 & U2 & Un -->|1. Request| Proxy

    %% Communication -> Storage & State & Master
    Proxy -->|2. Write Payload RPC| HDFS
    Proxy -.->|3. Update State| WS
    Proxy -->|4. Pass to Master| LM

    %% Master -> The Server Cluster (One arrow to the group)
    LM -->|5. Distribute Work| ServerCluster

    %% Server Cluster -> Storage
    ServerCluster -->|6. Commit Metadata| HDFS

    %% Server Cluster -> Users (Direct Receipt)
    ServerCluster -.->|7. Return Receipt| U1 & U2 & Un

    %% Styling
    classDef storage fill:#dbeafe,stroke:#3b82f6,stroke-width:2px;
    class HDFS,WS storage;
    classDef comm fill:#dcfce7,stroke:#22c55e,stroke-width:2px;
    class Proxy comm;
    classDef master fill:#fce7f3,stroke:#db2777,stroke-width:2px;
    class LM master;