package agent

// skills_hub_http.go：Skills Hub 与 GitHub API 的 HTTP 客户端配置、重试与退避策略，
// 环境变量命名与 copaw/agents/skills_hub.py 保持一致（COPAW_SKILLS_HUB_*、GITHUB_TOKEN）。

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// 与 copaw skills_hub.py 一致的环境变量名，便于同一机器上共用配置。
const (
	envHubBaseURL      = "COPAW_SKILLS_HUB_BASE_URL"
	envHubSearchPath   = "COPAW_SKILLS_HUB_SEARCH_PATH"
	envHubVersionPath  = "COPAW_SKILLS_HUB_VERSION_PATH"
	envHubDetailPath   = "COPAW_SKILLS_HUB_DETAIL_PATH"
	envHubFilePath     = "COPAW_SKILLS_HUB_FILE_PATH"
	envHubHTTPTimeout  = "COPAW_SKILLS_HUB_HTTP_TIMEOUT"
	envHubHTTPRetries  = "COPAW_SKILLS_HUB_HTTP_RETRIES"
	envHubBackoffBase  = "COPAW_SKILLS_HUB_HTTP_BACKOFF_BASE"
	envHubBackoffCap   = "COPAW_SKILLS_HUB_HTTP_BACKOFF_CAP"
	envGitHubToken     = "GITHUB_TOKEN"
	envGHToken         = "GH_TOKEN"
)

var retryableHTTP = map[int]struct{}{
	http.StatusRequestTimeout:      {}, // 408
	http.StatusConflict:            {}, // 409
	425:                            {},
	http.StatusTooManyRequests:     {}, // 429
	http.StatusInternalServerError: {}, // 500
	http.StatusBadGateway:          {}, // 502
	http.StatusServiceUnavailable:  {}, // 503
	http.StatusGatewayTimeout:      {}, // 504
}

type hubHTTPConfig struct {
	BaseURL      string
	SearchPath   string
	VersionPath  string
	DetailPath   string
	FilePath     string
	Timeout      time.Duration
	Retries      int
	BackoffBase  float64
	BackoffCap   float64
	GitHubToken  string
}

func loadHubHTTPConfig() hubHTTPConfig {
	cfg := hubHTTPConfig{
		BaseURL:     strings.TrimSuffix(getenvDefault(envHubBaseURL, "https://clawhub.ai"), "/"),
		SearchPath:  getenvDefault(envHubSearchPath, "/api/v1/search"),
		VersionPath: getenvDefault(envHubVersionPath, "/api/v1/skills/{slug}/versions/{version}"),
		DetailPath:  getenvDefault(envHubDetailPath, "/api/v1/skills/{slug}"),
		FilePath:    getenvDefault(envHubFilePath, "/api/v1/skills/{slug}/file"),
		Timeout:     15 * time.Second,
		Retries:     3,
		BackoffBase: 0.8,
		BackoffCap:  6,
	}
	if v := os.Getenv(envHubHTTPTimeout); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 3 {
			cfg.Timeout = time.Duration(f * float64(time.Second))
		}
	}
	if v := os.Getenv(envHubHTTPRetries); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.Retries = n
		}
	}
	if v := os.Getenv(envHubBackoffBase); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.1 {
			cfg.BackoffBase = f
		}
	}
	if v := os.Getenv(envHubBackoffCap); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.5 {
			cfg.BackoffCap = f
		}
	}
	cfg.GitHubToken = strings.TrimSpace(os.Getenv(envGitHubToken))
	if cfg.GitHubToken == "" {
		cfg.GitHubToken = strings.TrimSpace(os.Getenv(envGHToken))
	}
	return cfg
}

func getenvDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func joinHubURL(base, path string) string {
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(path, "/")
}

func backoffSeconds(attempt int, base, cap float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	sec := base * math.Pow(2, float64(max(0, attempt-1)))
	if sec > cap {
		sec = cap
	}
	return time.Duration(sec * float64(time.Second))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// hubHTTPFetch 带重试的 GET；maxBytes<=0 表示不限制响应体大小。
func hubHTTPFetch(ctx context.Context, cfg hubHTTPConfig, rawURL string, accept string, maxBytes int64) ([]byte, error) {
	attempts := cfg.Retries + 1
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		req.Header.Set("User-Agent", "gopaw-skills-hub/1.0")
		if u, err := url.Parse(rawURL); err == nil {
			host := strings.ToLower(u.Host)
			if cfg.GitHubToken != "" && strings.Contains(host, "api.github.com") {
				req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
			}
		}
		client := &http.Client{Timeout: cfg.Timeout}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < attempts {
				time.Sleep(backoffSeconds(attempt, cfg.BackoffBase, cfg.BackoffCap))
			}
			continue
		}
		body, readErr := func() ([]byte, error) {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
			}
			r := io.Reader(resp.Body)
			if maxBytes > 0 {
				r = io.LimitReader(resp.Body, maxBytes+1)
			}
			b, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			if maxBytes > 0 && int64(len(b)) > maxBytes {
				return nil, fmt.Errorf("response body exceeds %d bytes", maxBytes)
			}
			return b, nil
		}()
		if readErr == nil {
			return body, nil
		}
		lastErr = readErr
		if resp.StatusCode >= 400 {
			if _, ok := retryableHTTP[resp.StatusCode]; ok && attempt < attempts {
				time.Sleep(backoffSeconds(attempt, cfg.BackoffBase, cfg.BackoffCap))
				continue
			}
			return nil, lastErr
		}
		if attempt < attempts {
			time.Sleep(backoffSeconds(attempt, cfg.BackoffBase, cfg.BackoffCap))
			continue
		}
		return nil, lastErr
	}
	return nil, lastErr
}
