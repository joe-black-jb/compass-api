.PHONY: xbrl local terraform zip localstack lint fmt
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
	ENV=local go run ./batch/getXBRL.go

delS3:
	go run ./S3/deleteS3.go

deploy:
	sh ./scripts/deploy.sh $(TARGET)

news:
	ENV=local go run ./newsBatch/getNews.go

local:
	ENV=local go run ./local/local.go

up:
	@docker-compose up -d

down:
	@docker-compose down

start:
	@docker-compose start

stop:
	@docker-compose stop

zip:
	@GOOS=linux GOARCH=amd64 go build -o tmp/main cmd/compass-api/main.go
	@zip tmp/main.zip tmp/main
	@rm tmp/main

tf:
	@tflocal -chdir=terraform/localStack init
	@tflocal -chdir=terraform/localStack apply --auto-approve

local-build:
	docker build -t local-compass-api .

local-lambda-update:
  aws --endpoint-url=http://localhost:4566 lambda update-function-code --function-name compass-api-local --zip-file fileb://./tmp/main.zip
