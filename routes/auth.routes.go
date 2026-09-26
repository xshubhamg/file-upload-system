package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/xshubhamg/file-upload/controllers"
	"github.com/xshubhamg/file-upload/middlewares"
)

func AuthRoutes(router *gin.RouterGroup) {
	authRouter := router.Group("/auth")

	authRouter.POST("/login", controllers.HandleLogin)
	authRouter.POST("/register", controllers.HandleRegister)
	authRouter.POST("/logout", middlewares.AuthRequired, controllers.HandleLogout)
	authRouter.GET("/me", middlewares.AuthRequired, controllers.GetUser)
}
