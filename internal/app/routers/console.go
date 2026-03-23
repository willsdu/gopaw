package routers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ConsoleController struct{}

func (cc *ConsoleController) GetPushMessages(c *gin.Context) {
	// gopaw 暂未实现 push 消息存储；先返回空数组以兼容前端轮询。
	c.JSON(http.StatusOK, gin.H{"messages": []any{}})
}
