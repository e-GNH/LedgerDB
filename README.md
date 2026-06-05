

# LedgerDB Repo

## How to run

### Terminal 1
```
cd LedgerServer
go mod tidy
go run .
```

### Terminal 2
```
cd LedgerProxy
go mod tidy
go run cmd/server/server.go
```

### Terminal 3
```
cd StorageKernel
go mod tidy
go run .
```


