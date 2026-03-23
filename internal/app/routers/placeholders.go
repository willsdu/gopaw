package routers

import "github.com/gin-gonic/gin"

func notImplemented(c *gin.Context, feature string) {
	c.JSON(501, gin.H{
		"error": feature + " is not implemented in gopaw yet",
	})
}

type PlaceholderController struct{}

// 当前占位仅保留未来可能继续迁移的模块。
