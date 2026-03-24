package routers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 使用当前 Provider 的 OpenAI 兼容 /v1/chat/completions 流式接口，将增量映射为 AgentScope Runtime SSE 事件。

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmTokenUsage 上游返回的用量；流式末尾 chunk 可能才有 usage。
type llmTokenUsage struct {
	PromptTokens     int
	CompletionTokens int
}

func providerUsesOpenAICompat(p ProviderInfo) bool {
	cm := strings.ToLower(p.ChatModel)
	if strings.Contains(cm, "anthropic") {
		return false
	}
	return true
}

func chatCompletionsURL(base string) string {
	b := strings.TrimSuffix(strings.TrimSpace(base), "/")
	return b + "/chat/completions"
}

func usageFromOpenAIMap(u map[string]any) llmTokenUsage {
	if u == nil {
		return llmTokenUsage{}
	}
	return llmTokenUsage{
		PromptTokens:     anyToIntUsage(u["prompt_tokens"]),
		CompletionTokens: anyToIntUsage(u["completion_tokens"]),
	}
}

func anyToIntUsage(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return 0
	}
}

func runOpenAIChatStream(
	ctx context.Context,
	apiKey, url, model string,
	messages []openAIChatMessage,
	extra map[string]any,
	onDelta func(text string) error,
) (full string, usage llmTokenUsage, err error) {
	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	for k, v := range extra {
		if k == "model" || k == "messages" || k == "stream" {
			continue
		}
		body[k] = v
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
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return "", usage, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return "", usage, fmt.Errorf("chat completions %d: %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
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
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var raw map[string]any
		if json.Unmarshal([]byte(payload), &raw) != nil {
			continue
		}
		if u, ok := raw["usage"].(map[string]any); ok {
			lastU = usageFromOpenAIMap(u)
		}
		choices, _ := raw["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		ch0, ok := choices[0].(map[string]any)
		if !ok {
			continue
		}
		delta, _ := ch0["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		piece, _ := delta["content"].(string)
		if piece == "" {
			continue
		}
		acc.WriteString(piece)
		if onDelta != nil {
			if err := onDelta(piece); err != nil {
				return acc.String(), lastU, err
			}
		}
	}
	return acc.String(), lastU, nil
}

func runOpenAIChatNonStream(
	ctx context.Context,
	apiKey, url, model string,
	messages []openAIChatMessage,
	extra map[string]any,
) (string, llmTokenUsage, error) {
	var usage llmTokenUsage
	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	for k, v := range extra {
		if k == "model" || k == "messages" || k == "stream" {
			continue
		}
		body[k] = v
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
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 120 * time.Second}
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
		return "", usage, fmt.Errorf("chat completions %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", usage, err
	}
	usage = usageFromOpenAIMap(out.Usage)
	if len(out.Choices) == 0 {
		return "", usage, fmt.Errorf("empty choices from provider")
	}
	return out.Choices[0].Message.Content, usage, nil
}
