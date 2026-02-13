---
config:
  theme: neutral
---
flowchart TB
 subgraph Databases["Databases"]
        WS["World State<br>(Redis)<br>UserID → Amount"]
        Users["Users DB<br>(MySQL)<br>User Info"]
        Banks["Banks DB<br>(Redis)<br>wallet_id → bank_id"]
        Journal["Journal Store<br>(HDFS)<br>Transaction Log"]
        BlockInfo["BlockInfo Store<br>(MySQL)<br>Block Metadata"]
        Servers["Servers DB<br>(MySQL)<br>Server Status"]
  end
    Client["👤 Client"] -- "1. Send Request<br>Client-Bank Payload" --> CB["🏦 Commercial Bank<br>(KYC/AML)"]
    CB -- "2. Forward<br>Request Payload<br>with Encryption" --> Proxy["🔐 Ledger Proxy<br>(Auth &amp; Verify)"]
    Proxy -- "3a. Verify TX" --> Proxy
    Proxy -- "3b. Check Balance" --> WS
    Proxy -- "3c. Update Balance" --> WS
    Proxy -- "4. Forward<br>Proxy-Server Payload" --> Server["📚 Ledger Server<br>(Append)"]
    Server -- "5. Append Transaction<br>Append TX" --> Journal
    Server -- "6. Update Block Info" --> BlockInfo
    Server -- "6a. Response to client<br>Receipt Payload" --> CB
    CB -- "7. Response to Client" --> Client
    Proxy -. Query User Info .-> Users
    Proxy -. Query Bank Info .-> Banks
    Master -. Monitor Servers .-> Servers

    style Client fill:#e1f5ff
    style CB fill:#fff3e0
    style Proxy fill:#f3e5f5
    style Master fill:#e8f5e9
    style Server fill:#fce4ec
    style WS fill:#c8e6c9
    style Users fill:#bbdefb
    style Banks fill:#ffccbc
    style Journal fill:#ffe0b2
    style BlockInfo fill:#f0f4c3
    style Servers fill:#d1c4e9
    style Databases fill:#f5f5f5
    linkStyle 10 stroke:#000000