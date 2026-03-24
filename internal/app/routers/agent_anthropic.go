package routers

// 本文件实现 Anthropic Messages API（/v1/messages）的流式与非流式调用，
// 供 POST /api/agent/process 在激活模型为 Anthropic 时使用，与 copaw 侧 Anthropic 供应商行为对齐。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const anthropicAPIVersion = "2023-06-01"

func providerIsAnthropic(p ProviderInfo) bool {
	rid := strings.ToLower(strings.TrimSpace(p.ID))
	if rid == "anthropic" {
		return true
	}
	return strings.Contains(strings.ToLower(p.ChatModel), "anthropic")
}

func anthropicMessagesURL(base string) string {
	b := strings.TrimSuffix(strings.TrimSpace(base), "/")
	if strings.HasSuffix(b, "/v1") {
		return b + "/messages"
	}
	return b + "/v1/messages"
}

type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

func mergeAdjacentUserAssistant(msgs []openAIChatMessage) []openAIChatMessage {
	if len(msgs) == 0 {
		return nil
	}
	out := []openAIChatMessage{msgs[0]}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Role == out[len(out)-1].Role {
			out[len(out)-1].Content += "\n" + msgs[i].Content
		} else {
			out = append(out, msgs[i])
		}
	}
	return out
}

// openAIMessagesToAnthropic 将 chat/completions 风格消息转为 Messages API 的 system + messages。
func openAIMessagesToAnthropic(msgs []openAIChatMessage) (system string, apiMsgs []anthropicMessage) {
	var sysParts []string
	var tail []openAIChatMessage
	for _, m := range msgs {
		switch m.Role {
		case "system":
			sysParts = append(sysParts, m.Content)
		case "user", "assistant":
			tail = append(tail, m)
		}
	}
	system = strings.Join(sysParts, "\n\n")
	merged := mergeAdjacentUserAssistant(tail)
	if len(merged) > 0 && merged[0].Role == "assistant" {
		merged = append([]openAIChatMessage{{Role: "user", Content: "[conversation context]"}}, merged...)
	}
	for _, m := range merged {
		apiMsgs = append(apiMsgs, anthropicMessage{
			Role:    m.Role,
			Content: []anthropicBlock{{Type: "text", Text: m.Content}},
		})
	}
	return system, apiMsgs
}

func anthropicMaxTokens(extra map[string]any, p ProviderInfo) int64 {
	const def = int64(4096)
	if p.GenerateKw != nil {
		if v, ok := p.GenerateKw["max_tokens"]; ok {
			if n, ok := toInt64(v); ok && n > 0 {
				return n
			}
		}
	}
	if extra != nil {
		if v, ok := extra["max_tokens"]; ok {
			if n, ok := toInt64(v); ok && n > 0 {
				return n
			}
		}
	}
	return def
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int:
		return int64(x), true
	case int64:
		return x, true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func runAnthropicMessagesNonStream(
	ctx context.Context,
	apiKey, url, model string,
	system string,
	messages []anthropicMessage,
	maxTokens int64,
) (string, llmTokenUsage, error) {
	var usage llmTokenUsage
	body := map[string]any{
		"model":       model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"stream":      false,
	}
	if system != "" {
		body["system"] = system
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", usage, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", usage, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	client := &http.Client{Timeout: 120}
	resp, err := client.Do(req)
	if err != nil {
		return "", usage, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", usage, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", usage, fmt.Errorf("anthropic messages %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", usage, err
	}
	usage = llmTokenUsage{PromptTokens: out.Usage.InputTokens, CompletionTokens: out.Usage.OutputTokens}
	var acc strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			acc.WriteString(block.Text)
		}
	}
	if acc.Len() == 0 {
		return "", usage, fmt.Errorf("empty content from anthropic")
	}
	return acc.String(), usage, nil
}

func runAnthropicMessagesStream(
	ctx context.Context,
	apiKey, url, model string,
	system string,
	messages []anthropicMessage,
	maxTokens int64,
	onDelta func(text string) error,
) (full string, usage llmTokenUsage, err error) {
	body := map[string]any{
		"model":       model,
		"max_tokens":  maxTokens,
		"messages":    messages,
		"stream":      true,
	}
	if system != "" {
		body["system"] = system
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", usage, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", usage, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return "", usage, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", usage, fmt.Errorf("anthropic messages %d: %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
	}

	br := bufio.NewReader(resp.Body)
	var acc strings.Builder
	var lastU llmTokenUsage
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return acc.String(), lastU, err
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"delta"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		if ev.Type == "error" && ev.Error != nil {
			msg := ev.Error.Message
			if msg == "" {
				msg = ev.Error.Type
			}
			return acc.String(), lastU, fmt.Errorf("anthropic: %s", msg)
		}
		if ev.Type == "message_delta" {
			lastU = llmTokenUsage{
				PromptTokens:     ev.Delta.Usage.InputTokens,
				CompletionTokens: ev.Delta.Usage.OutputTokens,
			}
		}
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			acc.WriteString(ev.Delta.Text)
			if onDelta != nil {
				if err := onDelta(ev.Delta.Text); err != nil {
					return acc.String(), lastU, err
				}
			}
		}
	}
	return acc.String(), lastU, nil
}
