package routers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/willisdu/gopaw/internal/app/consolepush"
)

// ConsoleController 提供控制台相关 HTTP 接口，与 copaw 的 routers/console.py 对齐。
type ConsoleController struct{}

// GetPushMessages 处理 GET /api/console/push-messages。
// - 若带 query session_id：返回该会话专属队列中的消息，并在服务端删除（take，单标签消费）。
// - 若无 session_id：返回全局最近 maxConsolePushAgeSeconds 内的消息，不删除（多标签共享，与 Python get_recent 一致）。
func (cc *ConsoleController) GetPushMessages(c *gin.Context) {
	sessionID := c.Query("session_id")
	var msgs []consolepush.Message
	if sessionID != "" {
		msgs = consolepush.TakeForSession(sessionID)
	} else {
		msgs = consolepush.GetRecent(consolepush.DefaultMaxAgeSeconds)
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}
