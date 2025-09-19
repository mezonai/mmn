file ?= proto/*.proto

protogen:
	protoc -I proto --go_out=proto --go_opt=paths=source_relative --go-grpc_out=proto --go-grpc_opt=paths=source_relative ${file}

# Initialize configs and env from example file to make application ready to run, ignore if existed
config:
	@./scripts/init-configs.sh

# Run all services
dev:
	docker compose --profile dev up -d

# Run bootstrap node
bootstrap-node:
	docker compose --profile bootstrap up -d

# Run simple mmn node
single-node:
	docker compose --profile node up -d

# Run mmn node under monitoring
monitored-node:
	docker compose --profile monitored-node up -d

# Run central monitoring services
monitoring-center:
	docker compose --profile monitoring-center up -d

.PHONY: protogen config dev bootstrap-node single-node monitored-node monitoring-center