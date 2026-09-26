package main

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/xshubhamg/file-upload/routes"
	"github.com/xshubhamg/file-upload/utils"

	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	PORT := os.Getenv("PORT")
	if PORT == "" {
		PORT = "8080"
	}
	utils.InitJWT()
	utils.SetupDB()

	app := gin.Default()

	routes.SetupRoutes(app)
	app.Run(fmt.Sprintf(":%v", PORT))
}
