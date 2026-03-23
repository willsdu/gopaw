package routers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type VoiceController struct{}

var voiceUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (vc *VoiceController) VoiceIncoming(c *gin.Context) {
	// 简化版 TwiML 响应（先保证接口可调用）。
	twiml := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Say>Voice channel is available but not fully configured yet.</Say>
</Response>`
	c.Data(http.StatusOK, "application/xml", []byte(twiml))
}

func (vc *VoiceController) VoiceWS(c *gin.Context) {
	conn, err := voiceUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "websocket upgrade failed",
		})
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
	})

	for {
		msgType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		if writeErr := conn.WriteMessage(msgType, payload); writeErr != nil {
			return
		}
	}
}

func (vc *VoiceController) VoiceStatusCallback(c *gin.Context) {
	// 对齐 copaw 接口语义：回调可直接返回 204。
	c.Status(http.StatusNoContent)
}
