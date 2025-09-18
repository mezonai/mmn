file ?= proto/*.proto

protogen:
	protoc -I proto --go_out=proto --go_opt=paths=source_relative --go-grpc_out=proto --go-grpc_opt=paths=source_relative ${file}

dev:
	docker compose --env-file "./${ENV}" --profile dev up -d

bootstrap-node:
	docker compose --env-file "./${ENV}" --profile bootstrap up -d

single-node:
	docker compose --env-file "./${ENV}" --profile node up -d

monitored-node:
	docker compose --env-file "./${ENV}" --profile monitored-node up -d

monitoring-center:
	docker compose --env-file "./${ENV}" --profile monitoring-center up -d

.PHONY: protogen dev bootstrap-node single-node monitored-node monitoring-center