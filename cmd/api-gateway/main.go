package main

import (
	"fmt"
	"myAgent/internal/config"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	cfg := config.Load()
	fmt.Printf("config loaded: %+v\n", cfg)

	r.Run(":8080")
}
