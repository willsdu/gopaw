package routers

// MCP 配置持久化：与 copaw 一致，读写工作区根目录 config.json 中的 mcp.clients。
// 支持从旧版 gopaw 的 mcp_clients.json 一次性迁移；List/Get 响应对 env、headers 做脱敏（与 copaw _mask_env_value 对齐）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/willisdu/gopaw/internal/config"
)

func mainConfigJSONPath() string {
	return filepath.Join(config.WorkingDir(), "config.json")
}

func mcpLegacyStandalonePath() string {
	return filepath.Join(config.WorkingDir(), "mcp_clients.json")
}

func loadRootConfigMap() (map[string]any, error) {
	p := mainConfigJSONPath()
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func saveRootConfigMap(root map[string]any) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(mainConfigJSONPath(), b, 0o644)
}

func extractMCPClientsMap(root map[string]any) map[string]MCPClientInfo {
	mcpObj, _ := root["mcp"].(map[string]any)
	if mcpObj == nil {
		return map[string]MCPClientInfo{}
	}
	rawClients, _ := mcpObj["clients"].(map[string]any)
	if rawClients == nil {
		return map[string]MCPClientInfo{}
	}
	out := make(map[string]MCPClientInfo)
	for k, v := range rawClients {
		c, ok := mcpClientFromConfigValue(k, v)
		if !ok {
			continue
		}
		out[k] = c
	}
	return out
}

func mcpClientFromConfigValue(key string, v any) (MCPClientInfo, bool) {
	sub, ok := v.(map[string]any)
	if !ok {
		return MCPClientInfo{}, false
	}
	blob, err := json.Marshal(sub)
	if err != nil {
		return MCPClientInfo{}, false
	}
	var c MCPClientInfo
	if err := json.Unmarshal(blob, &c); err != nil {
		return MCPClientInfo{}, false
	}
	if ia, ok := sub["isActive"].(bool); ok && sub["enabled"] == nil {
		c.Enabled = ia
	}
	if u, ok := sub["baseUrl"].(string); ok && strings.TrimSpace(c.URL) == "" {
		c.URL = u
	}
	if typ, ok := sub["type"].(string); ok && strings.TrimSpace(c.Transport) == "" {
		c.Transport = typ
	}
	c.Key = key
	normalizeMCPTransport(&c)
	return c, true
}

func normalizeMCPTransport(c *MCPClientInfo) {
	t := strings.TrimSpace(strings.ToLower(c.Transport))
	alias := map[string]string{
		"streamablehttp": "streamable_http",
		"http":           "streamable_http",
	}
	if nt, ok := alias[t]; ok {
		c.Transport = nt
	} else if t != "" {
		c.Transport = t
	}
	if c.Transport == "" && strings.TrimSpace(c.URL) != "" && strings.TrimSpace(c.Command) == "" {
		c.Transport = "streamable_http"
	}
}

func setMCPClientsInRoot(root map[string]any, clients map[string]MCPClientInfo) {
	mcpObj, ok := root["mcp"].(map[string]any)
	if !ok || mcpObj == nil {
		mcpObj = map[string]any{}
		root["mcp"] = mcpObj
	}
	inner := make(map[string]any)
	for k, v := range clients {
		v.Key = k
		blob, err := json.Marshal(v)
		if err != nil {
			continue
		}
		var vm map[string]any
		if json.Unmarshal(blob, &vm) != nil {
			continue
		}
		delete(vm, "key")
		inner[k] = vm
	}
	mcpObj["clients"] = inner
}

// migrateStandaloneMCPFileIfNeeded：若 config.json 中尚无 mcp 客户端，则导入 mcp_clients.json 并写回 config.json。
func migrateStandaloneMCPFileIfNeeded(root map[string]any) (migrated bool, err error) {
	if len(extractMCPClientsMap(root)) > 0 {
		return false, nil
	}
	legacyPath := mcpLegacyStandalonePath()
	b, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var legacy map[string]MCPClientInfo
	if json.Unmarshal(b, &legacy) != nil || len(legacy) == 0 {
		return false, nil
	}
	for k, c := range legacy {
		c.Key = k
		normalizeMCPTransport(&c)
		legacy[k] = c
	}
	setMCPClientsInRoot(root, legacy)
	if err := saveRootConfigMap(root); err != nil {
		return false, err
	}
	_ = os.Rename(legacyPath, legacyPath+".bak")
	return true, nil
}

func loadMCPClientsFromConfig() (map[string]MCPClientInfo, error) {
	root, err := loadRootConfigMap()
	if err != nil {
		return nil, err
	}
	if _, err := migrateStandaloneMCPFileIfNeeded(root); err != nil {
		return nil, err
	}
	return extractMCPClientsMap(root), nil
}

func saveMCPClientsToConfig(m map[string]MCPClientInfo) error {
	root, err := loadRootConfigMap()
	if err != nil {
		return err
	}
	setMCPClientsInRoot(root, m)
	return saveRootConfigMap(root)
}

// maskSecretDisplay 与 copaw app/routers/mcp._mask_env_value 规则一致（按 rune 计长度以兼容非 ASCII）。
func maskSecretDisplay(value string) string {
	if value == "" {
		return value
	}
	rs := []rune(value)
	length := len(rs)
	if length <= 8 {
		return strings.Repeat("*", length)
	}
	prefixLen := 2
	if length > 2 && rs[2] == '-' {
		prefixLen = 3
	}
	prefix := string(rs[:prefixLen])
	suffix := string(rs[length-4:])
	maskedLen := length - prefixLen - 4
	if maskedLen < 4 {
		maskedLen = 4
	}
	return prefix + strings.Repeat("*", maskedLen) + suffix
}

func mcpClientMasked(c MCPClientInfo) MCPClientInfo {
	out := c
	if len(out.Env) > 0 {
		out.Env = maps.Clone(out.Env)
		for k, v := range out.Env {
			out.Env[k] = maskSecretDisplay(v)
		}
	}
	if len(out.Headers) > 0 {
		out.Headers = maps.Clone(out.Headers)
		for k, v := range out.Headers {
			out.Headers[k] = maskSecretDisplay(v)
		}
	}
	return out
}

func validateMCPClient(c *MCPClientInfo) error {
	t := c.Transport
	switch t {
	case "stdio":
		if strings.TrimSpace(c.Command) == "" {
			return errors.New("stdio MCP client requires non-empty command")
		}
	case "streamable_http", "sse":
		if strings.TrimSpace(c.URL) == "" {
			return fmt.Errorf("%s MCP client requires non-empty url", t)
		}
	case "":
		return errors.New("transport is required")
	default:
		// 未知 transport 仍允许写入，与控制台扩展兼容
	}
	return nil
}

func applyMCPCreateDefaults(body *MCPClientCreateRequest) MCPClientInfo {
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	tr := body.Transport
	if tr == "" {
		tr = "stdio"
	}
	headers := body.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	args := body.Args
	if args == nil {
		args = []string{}
	}
	env := body.Env
	if env == nil {
		env = map[string]string{}
	}
	return MCPClientInfo{
		Name:        body.Name,
		Description: body.Description,
		Enabled:     enabled,
		Transport:   tr,
		URL:         body.URL,
		Headers:     headers,
		Command:     body.Command,
		Args:        args,
		Env:         env,
		Cwd:         body.Cwd,
	}
}
