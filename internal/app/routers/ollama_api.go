package routers

// 通过 Ollama 守护进程原生 HTTP API（/api/tags、/api/pull、/api/delete）操作模型，
// 与 copaw 使用 Python SDK 的效果对齐。基址优先 OLLAMA_HOST，其次 providers.json 中 ollama 的 base_url（去掉 /v1）。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func resolveOllamaHTTPBase() string {
	if h := strings.TrimSpace(os.Getenv("OLLAMA_HOST")); h != "" {
		if !strings.HasPrefix(h, "http://") && !strings.HasPrefix(h, "https://") {
			h = "http://" + h
		}
		return strings.TrimSuffix(h, "/")
	}
	st, err := loadProvidersState()
	if err == nil {
		if p, ok := st.Providers["ollama"]; ok {
			b := strings.TrimSpace(p.BaseURL)
			if b != "" {
				b = strings.TrimSuffix(b, "/")
				b = strings.TrimSuffix(b, "/v1")
				return strings.TrimSuffix(b, "/")
			}
		}
	}
	return "http://127.0.0.1:11434"
}

func ollamaHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func ollamaHTTPClientNoTimeout() *http.Client {
	return &http.Client{Timeout: 0}
}

// ollamaListModelsFromDaemon 调用 GET /api/tags。
func ollamaListModelsFromDaemon(ctx context.Context) ([]OllamaModelResponse, error) {
	base := resolveOllamaHTTPBase()
	url := strings.TrimSuffix(base, "/") + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := ollamaHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama tags %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var parsed struct {
		Models []struct {
			Name       string `json:"name"`
			Model      string `json:"model"`
			Size       int64  `json:"size"`
			Digest     string `json:"digest"`
			ModifiedAt string `json:"modified_at"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	out := make([]OllamaModelResponse, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		n := m.Name
		if n == "" {
			n = m.Model
		}
		out = append(out, OllamaModelResponse{
			Name:       n,
			Size:       m.Size,
			Digest:     m.Digest,
			ModifiedAt: m.ModifiedAt,
		})
	}
	return out, nil
}

func ollamaDeleteOnDaemon(ctx context.Context, modelName string) error {
	base := resolveOllamaHTTPBase()
	url := strings.TrimSuffix(base, "/") + "/api/delete"
	raw, err := json.Marshal(map[string]string{"name": modelName})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ollamaHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ollama delete %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// ollamaPullStream POST /api/pull，按行解析 JSON；成功时返回 status==success 的一行；error 字段表示失败。
func ollamaPullStream(ctx context.Context, modelName string) error {
	base := resolveOllamaHTTPBase()
	url := strings.TrimSuffix(base, "/") + "/api/pull"
	raw, err := json.Marshal(map[string]string{"name": modelName})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ollamaHTTPClientNoTimeout().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ollama pull %d: %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
	}
	br := bufio.NewReader(resp.Body)
	var lastErr string
	sawSuccess := false
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if e, ok := m["error"].(string); ok && strings.TrimSpace(e) != "" {
			lastErr = e
		}
		if st, ok := m["status"].(string); ok && st == "success" {
			sawSuccess = true
		}
	}
	if lastErr != "" {
		return fmt.Errorf("%s", lastErr)
	}
	if sawSuccess {
		return nil
	}
	// 部分版本仅推进度行，结束时无 success；用列表再确认。
	models, err := ollamaListModelsFromDaemon(context.Background())
	if err != nil {
		return fmt.Errorf("pull stream ended without success: %w", err)
	}
	for _, m := range models {
		if m.Name == modelName {
			return nil
		}
	}
	return fmt.Errorf("pull stream ended without success and model %q not listed", modelName)
}
