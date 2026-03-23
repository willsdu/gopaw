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

type MCPClientInfo struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Enabled     bool              `json:"enabled"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
}

type MCPClientCreateRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Enabled     bool              `json:"enabled"`
	Transport   string            `json:"transport"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	Cwd         string            `json:"cwd"`
}

type MCPClientUpdateRequest struct {
	Name        *string           `json:"name,omitempty"`
	Description *string           `json:"description,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Transport   *string           `json:"transport,omitempty"`
	URL         *string           `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     *string           `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Cwd         *string           `json:"cwd,omitempty"`
}

type MCPController struct{}

func mcpPath() string {
	return filepath.Join(config.WorkingDir(), "mcp_clients.json")
}

func loadMCPClients() (map[string]MCPClientInfo, error) {
	out := map[string]MCPClientInfo{}
	b, err := os.ReadFile(mcpPath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]MCPClientInfo{}, nil
	}
	return out, nil
}

func saveMCPClients(m map[string]MCPClientInfo) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(mcpPath(), b, 0o644)
}

func (mc *MCPController) List(c *gin.Context) {
	m, err := loadMCPClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]MCPClientInfo, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	c.JSON(http.StatusOK, out)
}

func (mc *MCPController) Get(c *gin.Context) {
	key := c.Param("client_key")
	m, err := loadMCPClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	v, ok := m[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	c.JSON(http.StatusOK, v)
}

func (mc *MCPController) Create(c *gin.Context) {
	var body struct {
		ClientKey string                 `json:"client_key"`
		Client    MCPClientCreateRequest `json:"client"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if body.ClientKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_key is required"})
		return
	}
	m, err := loadMCPClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, exists := m[body.ClientKey]; exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MCP client already exists"})
		return
	}
	created := MCPClientInfo{
		Key:         body.ClientKey,
		Name:        body.Client.Name,
		Description: body.Client.Description,
		Enabled:     body.Client.Enabled,
		Transport:   body.Client.Transport,
		URL:         body.Client.URL,
		Headers:     body.Client.Headers,
		Command:     body.Client.Command,
		Args:        body.Client.Args,
		Env:         body.Client.Env,
		Cwd:         body.Client.Cwd,
	}
	m[body.ClientKey] = created
	if err := saveMCPClients(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (mc *MCPController) Update(c *gin.Context) {
	key := c.Param("client_key")
	var upd MCPClientUpdateRequest
	if err := c.ShouldBindJSON(&upd); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	m, err := loadMCPClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	v, ok := m[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	if upd.Name != nil {
		v.Name = *upd.Name
	}
	if upd.Description != nil {
		v.Description = *upd.Description
	}
	if upd.Enabled != nil {
		v.Enabled = *upd.Enabled
	}
	if upd.Transport != nil {
		v.Transport = *upd.Transport
	}
	if upd.URL != nil {
		v.URL = *upd.URL
	}
	if upd.Headers != nil {
		v.Headers = upd.Headers
	}
	if upd.Command != nil {
		v.Command = *upd.Command
	}
	if upd.Args != nil {
		v.Args = upd.Args
	}
	if upd.Env != nil {
		if v.Env == nil {
			v.Env = map[string]string{}
		}
		for k, val := range upd.Env {
			v.Env[k] = val
		}
	}
	if upd.Cwd != nil {
		v.Cwd = *upd.Cwd
	}
	m[key] = v
	if err := saveMCPClients(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

func (mc *MCPController) Toggle(c *gin.Context) {
	key := c.Param("client_key")
	m, err := loadMCPClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	v, ok := m[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	v.Enabled = !v.Enabled
	m[key] = v
	if err := saveMCPClients(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v)
}

func (mc *MCPController) Delete(c *gin.Context) {
	key := c.Param("client_key")
	m, err := loadMCPClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, ok := m[key]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	delete(m, key)
	if err := saveMCPClients(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "MCP client '" + key + "' deleted successfully"})
}
