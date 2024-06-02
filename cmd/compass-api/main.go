package main

import (
	"fmt"

	"github.com/conmass-api/internal/api"
	"github.com/conmass-api/internal/db"
)

func main() {
	fmt.Println("Hello World!")
	// DB接続
	db.Connect()
	// ルーター起動
	api.Router()
}