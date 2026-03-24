package routers

// 本文件实现 /api/config/*：读写工作区根目录的 app_config.json（心跳、渠道、LLM 路由、控制台、工具护栏等块）。
// 与 copaw 侧 app_config 持久化格式兼容，使用 map[string]any 保留未来扩展字段。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

type AppConfigState struct {
	Heartbeat  map[string]any   `json:"heartbeat"`
	Channels   json.RawMessage  `json:"channels"`
	LLMRouting map[string]any   `json:"llm_routing"`
	Console    map[string]any   `json:"console"`
	ToolGuard  map[string]any   `json:"tool_guard"`
}

type ConfigController struct{}

func defaultAppConfigState() AppConfigState {
	return AppConfigState{
		Heartbeat:  map[string]any{"enabled": false, "every": "6h", "target": "main"},
		Channels:   json.RawMessage("{}"),
		LLMRouting: map[string]any{},
		Console:    map[string]any{},
		ToolGuard:  map[string]any{"enabled": false, "rules": []any{}},
	}
}

func appConfigPath() string {
	return filepath.Join(config.WorkingDir(), "app_config.json")
}

func loadAppConfigState() (AppConfigState, error) {
	st := defaultAppConfigState()
	b, err := os.ReadFile(appConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return defaultAppConfigState(), nil
	}
	if len(bytes.TrimSpace(st.Channels)) == 0 || string(bytes.TrimSpace(st.Channels)) == "null" {
		st.Channels = json.RawMessage("{}")
	}
	return st, nil
}

func saveAppConfigState(st AppConfigState) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(appConfigPath(), b, 0o644)
}

func (cc *ConfigController) GetAll(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 合并 app_config 与 MCP 客户端列表（脱敏），便于控制台一次拉取。
	mcpList := []MCPClientInfo{}
	var mcpKeys []string
	if m, e := loadMCPClientsFromConfig(); e == nil {
		for k := range m {
			mcpKeys = append(mcpKeys, k)
		}
		sort.Strings(mcpKeys)
		for _, k := range mcpKeys {
			v := m[k]
			v.Key = k
			mcpList = append(mcpList, mcpClientMasked(v))
		}
	}
	chView := buildChannelsAPIResponse(parseChannelsBlob(st.Channels))
	c.JSON(http.StatusOK, gin.H{
		"heartbeat":     effectiveHeartbeatForAPI(st.Heartbeat),
		"channels":      chView,
		"llm_routing":   effectiveLLMRoutingForAPI(st.LLMRouting),
		"console":       st.Console,
		"tool_guard":    effectiveToolGuardForAPI(st.ToolGuard),
		"mcp_clients":   mcpList,
		"last_api":      MainConfigTopField("last_api"),
		"last_dispatch": MainConfigTopField("last_dispatch"),
	})
}

func (cc *ConfigController) GetHeartbeat(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveHeartbeatForAPI(st.Heartbeat))
}

func (cc *ConfigController) PutHeartbeat(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.Heartbeat = body
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveHeartbeatForAPI(st.Heartbeat))
}

func (cc *ConfigController) GetChannels(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stored := parseChannelsBlob(st.Channels)
	c.JSON(http.StatusOK, buildChannelsAPIResponse(stored))
}

func (cc *ConfigController) PutChannels(c *gin.Context) {
	raw, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body"})
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	var toSave map[string]any
	switch t := v.(type) {
	case map[string]any:
		toSave = stripIsBuiltinFromChannelsMap(t)
	case []any:
		arr := make([]map[string]any, 0, len(t))
		for _, it := range t {
			if m, ok := it.(map[string]any); ok {
				arr = append(arr, m)
			}
		}
		b, _ := json.Marshal(arr)
		toSave = parseChannelsBlob(b)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "channels must be object or array"})
		return
	}
	blob, err := marshalChannelsMap(toSave)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.Channels = blob
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, buildChannelsAPIResponse(toSave))
}

func (cc *ConfigController) GetAvailableChannels(c *gin.Context) {
	c.JSON(http.StatusOK, availableBuiltinChannels())
}

// GetChannelTypes 与 copaw GET /config/channels/types 一致（控制台 listChannelTypes）。
func (cc *ConfigController) GetChannelTypes(c *gin.Context) {
	c.JSON(http.StatusOK, availableBuiltinChannels())
}

func (cc *ConfigController) GetChannelByName(c *gin.Context) {
	name := strings.ToLower(strings.TrimSpace(c.Param("channel_name")))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing channel name"})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stored := parseChannelsBlob(st.Channels)
	bset := builtinChannelSet()
	_, builtin := bset[name]
	_, inStored := stored[name]
	if !builtin && !inStored {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Channel '" + c.Param("channel_name") + "' not found"})
		return
	}
	full := buildChannelsAPIResponse(stored)
	out, ok := full[name]
	if !ok {
		out = map[string]any{"enabled": false, "bot_prefix": "", "isBuiltin": builtin}
	}
	c.JSON(http.StatusOK, out)
}

func (cc *ConfigController) PutChannelByName(c *gin.Context) {
	name := strings.ToLower(strings.TrimSpace(c.Param("channel_name")))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing channel name"})
		return
	}
	bset := builtinChannelSet()
	_, builtin := bset[name]
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	body = cloneMapShallow(body)
	delete(body, "isBuiltin")
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stored := parseChannelsBlob(st.Channels)
	if !builtin && stored[name] == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Channel '" + c.Param("channel_name") + "' not found"})
		return
	}
	stored[name] = body
	blob, err := marshalChannelsMap(stored)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.Channels = blob
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, buildChannelsAPIResponse(stored)[name])
}

func (cc *ConfigController) GetLLMRouting(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveLLMRoutingForAPI(st.LLMRouting))
}

func (cc *ConfigController) PutLLMRouting(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.LLMRouting = body
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveLLMRoutingForAPI(st.LLMRouting))
}

func (cc *ConfigController) GetConsole(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st.Console)
}

func (cc *ConfigController) PutConsole(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.Console = body
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}

func (cc *ConfigController) GetToolGuard(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveToolGuardForAPI(st.ToolGuard))
}

func (cc *ConfigController) PutToolGuard(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.ToolGuard = persistToolGuardFromRequest(body)
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveToolGuardForAPI(st.ToolGuard))
}

// 以下路径与 copaw 控制台 securityApi 一致（/config/security/tool-guard）。

func (cc *ConfigController) GetSecurityToolGuard(c *gin.Context) {
	cc.GetToolGuard(c)
}

func (cc *ConfigController) PutSecurityToolGuard(c *gin.Context) {
	cc.PutToolGuard(c)
}

// GetBuiltinToolGuardRules 与 copaw dangerous_shell_commands 等内置规则结构一致（静态列表，供 UI 展示）。
func (cc *ConfigController) GetBuiltinToolGuardRules(c *gin.Context) {
	c.JSON(http.StatusOK, builtinToolGuardRulesForAPI())
}
