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