module LedgerDB

go 1.25.0

require google.golang.org/grpc v1.79.2

require google.golang.org/protobuf v1.36.11 // indirect

require (
	LedgerProxy v0.0.0-00010101000000-000000000000
	LedgerServer v0.0.0-00010101000000-000000000000
)

require (
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
)

// Add these replace directives to point to your local directories
replace LedgerServer => ./LedgerServer

replace StorageKernel => ./StorageKernel

replace LedgerProxy => ./LedgerProxy
