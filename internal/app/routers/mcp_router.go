package routers

// 本文件实现 /api/mcp/*：MCP 客户端 CRUD 与 toggle。
// 持久化与 copaw 一致：工作区 config.json 中的 mcp.clients（不再单独使用 mcp_clients.json，除非一次性从该文件迁移）。

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
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
	Enabled     *bool             `json:"enabled,omitempty"`
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

func (mc *MCPController) List(c *gin.Context) {
	m, err := loadMCPClientsFromConfig()
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
		v := m[k]
		v.Key = k
		out = append(out, mcpClientMasked(v))
	}
	c.JSON(http.StatusOK, out)
}

func (mc *MCPController) Get(c *gin.Context) {
	key := c.Param("client_key")
	m, err := loadMCPClientsFromConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	v, ok := m[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	v.Key = key
	c.JSON(http.StatusOK, mcpClientMasked(v))
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
	if body.Client.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client.name is required"})
		return
	}
	m, err := loadMCPClientsFromConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, exists := m[body.ClientKey]; exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MCP client already exists"})
		return
	}
	created := applyMCPCreateDefaults(&body.Client)
	created.Key = body.ClientKey
	normalizeMCPTransport(&created)
	if err := validateMCPClient(&created); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m[body.ClientKey] = created
	if err := saveMCPClientsToConfig(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, mcpClientMasked(created))
}

func (mc *MCPController) Update(c *gin.Context) {
	key := c.Param("client_key")
	var upd MCPClientUpdateRequest
	if err := c.ShouldBindJSON(&upd); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	m, err := loadMCPClientsFromConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	v, ok := m[key]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	v.Key = key
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
	normalizeMCPTransport(&v)
	if err := validateMCPClient(&v); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m[key] = v
	if err := saveMCPClientsToConfig(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, mcpClientMasked(v))
}

func (mc *MCPController) Toggle(c *gin.Context) {
	key := c.Param("client_key")
	m, err := loadMCPClientsFromConfig()
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
	v.Key = key
	m[key] = v
	if err := saveMCPClientsToConfig(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, mcpClientMasked(v))
}

func (mc *MCPController) Delete(c *gin.Context) {
	key := c.Param("client_key")
	m, err := loadMCPClientsFromConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, ok := m[key]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP client '" + key + "' not found"})
		return
	}
	delete(m, key)
	if err := saveMCPClientsToConfig(m); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "MCP client '" + key + "' deleted successfully"})
}
