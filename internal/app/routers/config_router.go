package routers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

type AppConfigState struct {
	Heartbeat  map[string]any   `json:"heartbeat"`
	Channels   []map[string]any `json:"channels"`
	LLMRouting map[string]any   `json:"llm_routing"`
	Console    map[string]any   `json:"console"`
	ToolGuard  map[string]any   `json:"tool_guard"`
}

type ConfigController struct{}

func defaultAppConfigState() AppConfigState {
	return AppConfigState{
		Heartbeat:  map[string]any{"enabled": false, "every": "6h", "target": "main"},
		Channels:   []map[string]any{},
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
	c.JSON(http.StatusOK, st)
}

func (cc *ConfigController) GetHeartbeat(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st.Heartbeat)
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
	c.JSON(http.StatusOK, body)
}

func (cc *ConfigController) GetChannels(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st.Channels)
}

func (cc *ConfigController) PutChannels(c *gin.Context) {
	var body []map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	st.Channels = body
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}

func (cc *ConfigController) GetAvailableChannels(c *gin.Context) {
	c.JSON(http.StatusOK, []string{
		"console", "telegram", "discord", "voice", "feishu", "dingtalk",
	})
}

func (cc *ConfigController) GetLLMRouting(c *gin.Context) {
	st, err := loadAppConfigState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st.LLMRouting)
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
	c.JSON(http.StatusOK, body)
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
	c.JSON(http.StatusOK, st.ToolGuard)
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
	st.ToolGuard = body
	if err := saveAppConfigState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}
