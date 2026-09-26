package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/xshubhamg/file-upload/controllers"
	"github.com/xshubhamg/file-upload/middlewares"
)

func FileRoutes(router *gin.RouterGroup) {
	fileRouter := router.Group("/file")

	fileRouter.POST("/upload", middlewares.AuthRequired, controllers.UploadFile)
	fileRouter.GET("", middlewares.AuthRequired, controllers.ListFiles)
	fileRouter.GET("/", middlewares.AuthRequired, controllers.ListFiles)
	fileRouter.GET("/:filename", middlewares.AuthRequired, controllers.GetFile)
	fileRouter.DELETE("/:filename", middlewares.AuthRequired, controllers.DeleteFile)
}
