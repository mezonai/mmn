file ?= proto/*.proto

protogen:
	protoc -I proto --go_out=proto --go_opt=paths=source_relative --go-grpc_out=proto --go-grpc_opt=paths=source_relative ${file}

dev:
	docker compose --profile dev up -d

bootstrap-node:
	docker compose --profile bootstrap up -d

regular-node:
	docker compose --profile node up -d

monitored-node:
	docker compose --profile monitored-node up -d

monitoring-center:
	docker compose --profile monitoring-center up -d

loki-client:
	@cd ./monitoring/loki && ./add-client.sh && cd -

.PHONY: protogen dev bootstrap-node regular-node monitored-node monitoring-center loki-client