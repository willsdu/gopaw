package routers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// AgentAppController 提供与 agentscope_runtime.AgentApp 对齐的 HTTP 端点，
// 供 copaw 控制台 @agentscope-ai/chat 通过 POST /api/agent/process 发起对话（SSE）。

type AgentAppController struct{}

func (a *AgentAppController) Root(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":        "gopaw",
		"description": "GoPaw agent HTTP surface (OpenAI-compatible backend)",
		"framework":   "gopaw",
	})
}

func (a *AgentAppController) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *AgentAppController) Shutdown(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "shutdown not applicable in gopaw"})
}

func (a *AgentAppController) AdminStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "running", "engine": "gopaw"})
}

func (a *AgentAppController) AdminShutdown(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "ignored"})
}

// Process POST /api/agent/process — SSE（stream=true）或 JSON（stream=false）。
func (a *AgentAppController) Process(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_json", "message": err.Error()}})
		return
	}
	stream := true
	if v, ok := raw["stream"].(bool); ok {
		stream = v
	}
	sessionID := normSessionKey(strings.TrimSpace(fmt.Sprint(raw["session_id"])))
	userText := extractLastUserText(raw["input"])
	if strings.TrimSpace(userText) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "empty_input", "message": "no user text in input"}})
		return
	}
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "config", "message": err.Error()}})
		return
	}
	p, ok := st.Providers[st.Active.ProviderID]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "model_not_configured", "message": "active provider not found"}})
		return
	}
	if strings.TrimSpace(st.Active.Model) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "model_not_configured", "message": "no active model"}})
		return
	}
	if strings.TrimSpace(p.APIKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "missing_api_key", "message": "configure API key for provider " + st.Active.ProviderID}})
		return
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "missing_base_url", "message": "provider base_url is empty"}})
		return
	}

	hist := globalSessions.getCopy(sessionID)
	apiMsgs := make([]openAIChatMessage, 0, len(hist)+1)
	for _, m := range hist {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "system" {
			continue
		}
		apiMsgs = append(apiMsgs, openAIChatMessage{Role: m.Role, Content: m.Content})
	}
	apiMsgs = append(apiMsgs, openAIChatMessage{Role: "user", Content: userText})

	ctx := c.Request.Context()

	// Anthropic：走 Messages API；其余非 OpenAI 兼容供应商仍返回 501。
	if providerIsAnthropic(p) {
		if !stream {
			text, err := executeNonStreamChat(ctx, st, p, apiMsgs)
			if err != nil {
				if errors.Is(err, errUnsupportedChatProvider) {
					c.JSON(http.StatusNotImplemented, gin.H{"error": gin.H{"code": "provider_not_supported", "message": err.Error()}})
					return
				}
				if strings.Contains(err.Error(), "no user/assistant messages") {
					c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "empty_messages", "message": err.Error()}})
					return
				}
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "upstream", "message": err.Error()}})
				return
			}
			globalSessions.appendUserThenAssistant(sessionID, userText, text)
			persistProcessTurnToChats(sessionID, userText, text)
			maybeConsolePushAfterProcess(raw, sessionID, text)
			c.JSON(http.StatusOK, snapshotOutput(text, sessionID))
			return
		}
		extra := providerExtraMap(p)
		sys, ams := openAIMessagesToAnthropic(apiMsgs)
		if len(ams) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "empty_messages", "message": "no user/assistant messages for anthropic"}})
			return
		}
		aurl := anthropicMessagesURL(p.BaseURL)
		maxTok := anthropicMaxTokens(extra, p)
		w := c.Writer
		header := w.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		rid := randomID("response")
		mid := randomID("msg")
		now := int(time.Now().Unix())
		writeSSE := func(v any) {
			b, _ := json.Marshal(v)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
			if fl != nil {
				fl.Flush()
			}
		}
		writeSSE(map[string]any{"id": rid, "object": "response", "status": "created", "created_at": now})
		writeSSE(map[string]any{
			"id": mid, "object": "message", "type": "message", "role": "assistant", "status": "created",
		})
		var full strings.Builder
		onDelta := func(piece string) error {
			full.WriteString(piece)
			writeSSE(map[string]any{
				"object": "content", "type": "text", "status": "in_progress", "index": 0,
				"delta": true, "text": piece, "msg_id": mid,
			})
			return nil
		}
		text, u, err := runAnthropicMessagesStream(ctx, p.APIKey, aurl, st.Active.Model, sys, ams, maxTok, onDelta)
		if err != nil {
			writeSSE(map[string]any{"error": map[string]any{"code": "upstream", "message": err.Error()}})
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			if fl != nil {
				fl.Flush()
			}
			return
		}
		if text == "" {
			text = full.String()
		}
		writeSSE(map[string]any{
			"object": "content", "type": "text", "status": "completed", "index": 0,
			"delta": false, "text": text, "msg_id": mid,
		})
		writeSSE(map[string]any{"id": mid, "object": "message", "status": "completed"})
		writeSSE(map[string]any{"id": rid, "object": "response", "status": "completed", "completed_at": int(time.Now().Unix())})
		writeSSE(snapshotOutputMap(text))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
		globalSessions.appendUserThenAssistant(sessionID, userText, text)
		persistProcessTurnToChats(sessionID, userText, text)
		RecordTokenUsage(st.Active.ProviderID, st.Active.Model, u.PromptTokens, u.CompletionTokens)
		maybeConsolePushAfterProcess(raw, sessionID, text)
		return
	}

	if !providerUsesOpenAICompat(p) {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": gin.H{
				"code":    "provider_not_supported",
				"message": "gopaw /agent/process supports OpenAI-compatible or Anthropic Messages API; configure provider accordingly",
			},
		})
		return
	}

	url := chatCompletionsURL(p.BaseURL)
	extra := providerExtraMap(p)

	if !stream {
		text, err := executeNonStreamChat(ctx, st, p, apiMsgs)
		if err != nil {
			if errors.Is(err, errUnsupportedChatProvider) {
				c.JSON(http.StatusNotImplemented, gin.H{"error": gin.H{"code": "provider_not_supported", "message": err.Error()}})
				return
			}
			c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"code": "upstream", "message": err.Error()}})
			return
		}
		globalSessions.appendUserThenAssistant(sessionID, userText, text)
		persistProcessTurnToChats(sessionID, userText, text)
		maybeConsolePushAfterProcess(raw, sessionID, text)
		c.JSON(http.StatusOK, snapshotOutput(text, sessionID))
		return
	}

	// ---- SSE ----
	w := c.Writer
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)

	rid := randomID("response")
	mid := randomID("msg")
	now := int(time.Now().Unix())

	writeSSE := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		if fl != nil {
			fl.Flush()
		}
	}

	writeSSE(map[string]any{"id": rid, "object": "response", "status": "created", "created_at": now})
	writeSSE(map[string]any{
		"id": mid, "object": "message", "type": "message", "role": "assistant", "status": "created",
	})

	var full strings.Builder
	onDelta := func(piece string) error {
		full.WriteString(piece)
		writeSSE(map[string]any{
			"object": "content", "type": "text", "status": "in_progress", "index": 0,
			"delta": true, "text": piece, "msg_id": mid,
		})
		return nil
	}

	text, u, err := runOpenAIChatStream(ctx, p.APIKey, url, st.Active.Model, apiMsgs, extra, onDelta)
	if err != nil {
		writeSSE(map[string]any{"error": map[string]any{"code": "upstream", "message": err.Error()}})
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
		return
	}
	if text == "" {
		text = full.String()
	}

	writeSSE(map[string]any{
		"object": "content", "type": "text", "status": "completed", "index": 0,
		"delta": false, "text": text, "msg_id": mid,
	})
	writeSSE(map[string]any{"id": mid, "object": "message", "status": "completed"})
	writeSSE(map[string]any{"id": rid, "object": "response", "status": "completed", "completed_at": int(time.Now().Unix())})
	// 与 runtime 文档示例兼容：部分客户端直接读 output[0].content[0].text
	writeSSE(snapshotOutputMap(text))
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if fl != nil {
		fl.Flush()
	}
	globalSessions.appendUserThenAssistant(sessionID, userText, text)
	persistProcessTurnToChats(sessionID, userText, text)
	RecordTokenUsage(st.Active.ProviderID, st.Active.Model, u.PromptTokens, u.CompletionTokens)
	maybeConsolePushAfterProcess(raw, sessionID, text)
}

func snapshotOutput(text, sessionID string) map[string]any {
	m := snapshotOutputMap(text)
	m["session_id"] = sessionID
	return m
}

func snapshotOutputMap(text string) map[string]any {
	return map[string]any{
		"output": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": text},
				},
			},
		},
	}
}

func extractLastUserText(input any) string {
	arr, ok := input.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	for i := len(arr) - 1; i >= 0; i-- {
		m, ok := arr[i].(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role != "" && role != "user" {
			continue
		}
		t := textFromMessageContent(m["content"])
		if strings.TrimSpace(t) != "" {
			return t
		}
	}
	return ""
}

func textFromMessageContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch pm["type"] {
			case "text":
				if t, ok := pm["text"].(string); ok {
					b.WriteString(t)
				}
			case "input_text":
				if t, ok := pm["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}
