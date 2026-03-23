package routers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

type ToolInfo struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

type ToolsController struct{}

func toolsPath() string {
	return filepath.Join(config.WorkingDir(), "tools.json")
}

func defaultTools() map[string]ToolInfo {
	return map[string]ToolInfo{
		"read_file":  {Name: "read_file", Enabled: true, Description: "Read file content"},
		"write_file": {Name: "write_file", Enabled: true, Description: "Write file content"},
		"shell":      {Name: "shell", Enabled: true, Description: "Run shell command"},
	}
}

func loadTools() (map[string]ToolInfo, error) {
	def := defaultTools()
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
	return m, nil
}

func saveTools(m map[string]ToolInfo) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
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
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ToolInfo, 0, len(keys))
	for _, k := range keys {
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
