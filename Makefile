file ?= proto/*.proto
env ?= .env

protogen:
	protoc -I proto --go_out=proto --go_opt=paths=source_relative --go-grpc_out=proto --go-grpc_opt=paths=source_relative ${file}

# Initialize configs and env from example file to make application ready to run, ignore if existed
config:
	@./scripts/init-configs.sh

# Run all services
dev:
	@echo "Starting dev services..."
	@docker compose --env-file=${env} --profile dev up -d

# Run bootstrap node
bootstrap-node:
	@echo "Starting bootstrap node..."
	@docker compose --env-file=${env} --profile bootstrap up -d

# Run simple mmn node
single-node:
	@echo "Starting single node..."
	@docker compose --env-file=${env} --profile node up -d

# Run mmn node under monitoring
monitored-node:
	@echo "Starting monitored node..."
	@docker compose --env-file=${env} --profile monitored-node up -d

# Run central monitoring services
monitoring-center:
	@echo "Starting monitoring services..."
	@docker compose --env-file=${env} --profile monitoring-center up -d

.PHONY: protogen config dev bootstrap-node single-node monitored-node monitoring-center