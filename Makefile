# 変数
GO := go
GOFMT := golint
APP_DIR := ./scripts
APP_NAME := migration

migrate:
	$(GO) run $(APP_DIR)/$(APP_NAME).go