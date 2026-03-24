package routers

// 本文件实现 /api/tools/*：tools.json 中各内置工具的启用状态列表与 PATCH 切换。
// 内置工具名称、默认描述与 copaw config.ToolsConfig.builtin_tools 一致（见 copaw/config/config.py）。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"github.com/willisdu/gopaw/internal/config"
)

type ToolInfo struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

type ToolsController struct{}

// 与 copaw ToolsConfig.builtin_tools 的默认键顺序一致（控制台列表顺序）。
var builtinToolOrder = []string{
	"execute_shell_command",
	"read_file",
	"write_file",
	"edit_file",
	"browser_use",
	"desktop_screenshot",
	"send_file_to_user",
	"get_current_time",
	"get_token_usage",
}

func toolsPath() string {
	return filepath.Join(config.WorkingDir(), "tools.json")
}

func defaultBuiltinTools() map[string]ToolInfo {
	return map[string]ToolInfo{
		"execute_shell_command": {Name: "execute_shell_command", Enabled: true, Description: "Execute shell commands"},
		"read_file":             {Name: "read_file", Enabled: true, Description: "Read file contents"},
		"write_file":            {Name: "write_file", Enabled: true, Description: "Write content to file"},
		"edit_file":             {Name: "edit_file", Enabled: true, Description: "Edit file using find-and-replace"},
		"browser_use":           {Name: "browser_use", Enabled: true, Description: "Browser automation and web interaction"},
		"desktop_screenshot":    {Name: "desktop_screenshot", Enabled: true, Description: "Capture desktop screenshots"},
		"send_file_to_user":     {Name: "send_file_to_user", Enabled: true, Description: "Send files to user"},
		"get_current_time":      {Name: "get_current_time", Enabled: true, Description: "Get current date and time"},
		"get_token_usage":       {Name: "get_token_usage", Enabled: true, Description: "Get llm token usage"},
	}
}

func mergeBuiltinTools(loaded map[string]ToolInfo) map[string]ToolInfo {
	out := defaultBuiltinTools()
	for k, v := range loaded {
		if k == "shell" {
			if t, ok := out["execute_shell_command"]; ok {
				t.Enabled = v.Enabled
				out["execute_shell_command"] = t
			}
			continue
		}
		if t, ok := out[k]; ok {
			t.Enabled = v.Enabled
			if v.Description != "" {
				t.Description = v.Description
			}
			out[k] = t
		}
	}
	return out
}

func loadTools() (map[string]ToolInfo, error) {
	def := defaultBuiltinTools()
	b, err := os.ReadFile(toolsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return def, nil
		}
		return nil, err
	}
	var m map[string]ToolInfo
	if err := json.Unmarshal(b, &m); err != nil {
		return def, nil
	}
	if len(m) == 0 {
		return def, nil
	}
	return mergeBuiltinTools(m), nil
}

func saveTools(m map[string]ToolInfo) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	norm := make(map[string]ToolInfo, len(builtinToolOrder))
	for _, k := range builtinToolOrder {
		if t, ok := m[k]; ok {
			t.Name = k
			norm[k] = t
		}
	}
	b, err := json.MarshalIndent(norm, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(toolsPath(), b, 0o644)
}

func (tc *ToolsController) ListTools(c *gin.Context) {
	m, err := loadTools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]ToolInfo, 0, len(builtinToolOrder))
	for _, k := range builtinToolOrder {
		out = append(out, m[k])
	}
	c.JSON(http.StatusOK, out)
}

func (tc *ToolsController) ToggleTool(c *gin.Context) {
	toolName := c.Param("tool_name")
	m, err := loadTools()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	t, ok := m[toolName]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Tool '" + toolName + "' not found"})
		return
	}
	t.Enabled = !t.Enabled
	m[toolName] = t
	if err := saveTools(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}
