

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
go run ./cmd/server/
```

### Terminal 3
```
cd StorageKernel
go mod tidy
go run .
```
### Terminal 4 (for AML)
```
cd aml_service
uvicorn main:app --reload
```



