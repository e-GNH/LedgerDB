.PHONY: run-all server proxy kernel aml logs-dir

# Ensure the logs directory exists before running services
logs-dir:
	mkdir -p logs

server: logs-dir
	cd LedgerServer && go mod tidy && go run . > ../logs/server.log 2>&1

proxy: logs-dir
	cd LedgerProxy && go mod tidy && go run ./cmd/server/ > ../logs/proxy.log 2>&1

kernel: logs-dir
	cd StorageKernel && go mod tidy && go run . > ../logs/kernel.log 2>&1

aml: logs-dir
	cd aml_service && uvicorn main:app --reload > ../logs/aml.log 2>&1

run-all:
	make -j 4 server proxy kernel aml