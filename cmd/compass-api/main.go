package main

import (
	"fmt"

	"github.com/compass-api/internal/api"
	"github.com/compass-api/internal/database"
)

func main() {
	fmt.Println("Hello World!")
	// DB接続
	database.Connect()
	// ルーター起動
	api.Router()
}