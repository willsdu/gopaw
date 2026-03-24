package routers

// POST /api/skills/ai/optimize/stream：与 copaw skills_stream 一致，使用当前激活模型流式优化技能正文。
// SSE 仅使用 `data: {...}\n\n`（无 event: 行），增量字段为 text，结束为 done:true。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type SkillOptimizeStreamRequest struct {
	Content  string `json:"content"`
	Language string `json:"language,omitempty"`
}

type SkillsStreamController struct{}

func writeSkillOptimizeData(w http.ResponseWriter, fl http.Flusher, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
	if fl != nil {
		fl.Flush()
	}
}

func (sc *SkillsStreamController) OptimizeSkillStream(c *gin.Context) {
	var req SkillOptimizeStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	lang := strings.TrimSpace(strings.ToLower(req.Language))
	if lang == "" {
		lang = "en"
	}
	system := skillOptimizeSystemPrompt(lang)

	w := c.Writer
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)

	ctx := c.Request.Context()
	st, err := loadProvidersState()
	if err != nil {
		writeSkillOptimizeData(w, fl, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[st.Active.ProviderID]
	if !ok || strings.TrimSpace(st.Active.Model) == "" || strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.BaseURL) == "" {
		writeSkillOptimizeData(w, fl, gin.H{
			"error": "No AI model configured. Please configure in Settings.",
		})
		return
	}

	msgs := []openAIChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: req.Content},
	}
	extra := providerExtraMap(p)

	if providerIsAnthropic(p) {
		aurl := anthropicMessagesURL(p.BaseURL)
		maxTok := anthropicMaxTokens(extra, p)
		ams := []anthropicMessage{
			{Role: "user", Content: []anthropicBlock{{Type: "text", Text: req.Content}}},
		}
		var acc strings.Builder
		full, u, err := runAnthropicMessagesStream(ctx, p.APIKey, aurl, st.Active.Model, system, ams, maxTok, func(piece string) error {
			acc.WriteString(piece)
			writeSkillOptimizeData(w, fl, gin.H{"text": piece})
			return nil
		})
		if err != nil {
			writeSkillOptimizeData(w, fl, gin.H{"error": fmt.Sprintf("Failed to optimize skill: %v", err)})
			return
		}
		if strings.TrimSpace(full) == "" {
			full = acc.String()
		}
		RecordTokenUsage(st.Active.ProviderID, st.Active.Model, u.PromptTokens, u.CompletionTokens)
		writeSkillOptimizeData(w, fl, gin.H{"done": true})
		notifyConsoleIfConfigured("Skill optimize stream finished (Anthropic), length=%d chars", len(strings.TrimSpace(full)))
		return
	}

	if !providerUsesOpenAICompat(p) {
		writeSkillOptimizeData(w, fl, gin.H{"error": "Active provider is not OpenAI-compatible; switch model or use Anthropic/OpenAI-compatible endpoint."})
		return
	}

	url := chatCompletionsURL(p.BaseURL)
	var acc strings.Builder
	full, u, err := runOpenAIChatStream(ctx, p.APIKey, url, st.Active.Model, msgs, extra, func(piece string) error {
		acc.WriteString(piece)
		writeSkillOptimizeData(w, fl, gin.H{"text": piece})
		return nil
	})
	if err != nil {
		writeSkillOptimizeData(w, fl, gin.H{"error": fmt.Sprintf("Failed to optimize skill: %v", err)})
		return
	}
	if strings.TrimSpace(full) == "" {
		full = acc.String()
	}
	RecordTokenUsage(st.Active.ProviderID, st.Active.Model, u.PromptTokens, u.CompletionTokens)
	writeSkillOptimizeData(w, fl, gin.H{"done": true})
	notifyConsoleIfConfigured("Skill optimize stream finished (OpenAI-compatible), length=%d chars", len(strings.TrimSpace(full)))
}
