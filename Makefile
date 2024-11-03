.PHONY: xbrl
# 変数
GO := go
APP_DIR := ./scripts
APP_NAME := migration

migrate:
	$(GO) run $(APP_DIR)/$(APP_NAME).go

lint:
	go vet ./...

fmt:
	go fmt ./...

xbrl:
	go run ./batch/getXBRL.go

delS3:
	go run ./S3/deleteS3.go

deploy:
	sh ./scripts/deploy.sh $(TARGET)
