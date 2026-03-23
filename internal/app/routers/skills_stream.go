package routers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type SkillOptimizeStreamRequest struct {
	Content  string `json:"content"`
	Language string `json:"language,omitempty"`
}

type SkillsStreamController struct{}

func writeSSE(c *gin.Context, event string, payload any) {
	b, _ := json.Marshal(payload)
	if event != "" {
		_, _ = fmt.Fprintf(c.Writer, "event: %s\n", event)
	}
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", string(b))
	c.Writer.Flush()
}

func (sc *SkillsStreamController) OptimizeSkillStream(c *gin.Context) {
	var req SkillOptimizeStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	lang := req.Language
	if lang == "" {
		lang = "en"
	}

	// 模拟分片输出，结构上兼容前端 SSE 消费。
	writeSSE(c, "start", gin.H{
		"ok":       true,
		"language": lang,
	})

	time.Sleep(150 * time.Millisecond)
	writeSSE(c, "chunk", gin.H{
		"text": "# Optimized Skill\n",
	})

	time.Sleep(150 * time.Millisecond)
	writeSSE(c, "chunk", gin.H{
		"text": req.Content,
	})

	time.Sleep(80 * time.Millisecond)
	writeSSE(c, "done", gin.H{
		"ok": true,
	})
}
