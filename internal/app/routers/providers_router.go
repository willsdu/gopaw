package routers

// 本文件实现 /api/models/*：多厂商 LLM 配置、自定义供应商、模型列表、连通性探测占位与当前激活模型。
// 持久化文件为 providers.json。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProviderInfo struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	BaseURL      string         `json:"base_url,omitempty"`
	APIKeyPrefix string         `json:"api_key_prefix,omitempty"`
	ChatModel    string         `json:"chat_model,omitempty"`
	Models       []ModelInfo    `json:"models"`
	GenerateKw   map[string]any `json:"generate_kwargs,omitempty"`
	APIKey       string         `json:"api_key,omitempty"`
	Custom       bool           `json:"custom"`
}

type ActiveModel struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

type ActiveModelsInfo struct {
	ActiveLLM ActiveModel `json:"active_llm"`
}

type providersState struct {
	Providers map[string]ProviderInfo `json:"providers"`
	Active    ActiveModel             `json:"active"`
}

type ProviderConfigRequest struct {
	APIKey     *string        `json:"api_key"`
	BaseURL    *string        `json:"base_url"`
	ChatModel  *string        `json:"chat_model"`
	GenerateKw map[string]any `json:"generate_kwargs"`
}

type CreateCustomProviderRequest struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	DefaultBaseURL string      `json:"default_base_url"`
	APIKeyPrefix   string      `json:"api_key_prefix"`
	ChatModel      string      `json:"chat_model"`
	Models         []ModelInfo `json:"models"`
}

type AddModelRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TestConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type DiscoverModelsResponse struct {
	Success    bool        `json:"success"`
	Models     []ModelInfo `json:"models"`
	Message    string      `json:"message,omitempty"`
	AddedCount int         `json:"added_count,omitempty"`
}

type ModelSlotRequest struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
}

type ProvidersController struct{}

func providersPath() string {
	return filepath.Join(config.WorkingDir(), "providers.json")
}

func defaultOllamaProvider() ProviderInfo {
	return ProviderInfo{
		ID:           "ollama",
		Name:         "Ollama",
		BaseURL:      "http://127.0.0.1:11434/v1",
		APIKeyPrefix: "",
		ChatModel:    "OpenAIChatModel",
		Models: []ModelInfo{
			{ID: "llama3.2", Name: "llama3.2"},
			{ID: "qwen2.5", Name: "qwen2.5"},
		},
		Custom: false,
	}
}

func defaultProvidersState() providersState {
	openai := ProviderInfo{
		ID:           "openai",
		Name:         "OpenAI",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyPrefix: "sk-",
		ChatModel:    "OpenAIChatModel",
		Models: []ModelInfo{
			{ID: "gpt-4o-mini", Name: "gpt-4o-mini"},
			{ID: "gpt-4o", Name: "gpt-4o"},
		},
		Custom: false,
	}
	anthropic := ProviderInfo{
		ID:           "anthropic",
		Name:         "Anthropic",
		BaseURL:      "https://api.anthropic.com",
		APIKeyPrefix: "sk-ant-",
		ChatModel:    "AnthropicChatModel",
		Models: []ModelInfo{
			{ID: "claude-3-5-sonnet-20241022", Name: "claude-3.5-sonnet"},
		},
		Custom: false,
	}
	ollama := defaultOllamaProvider()
	return providersState{
		Providers: map[string]ProviderInfo{
			"openai":    openai,
			"anthropic": anthropic,
			"ollama":    ollama,
		},
		Active: ActiveModel{
			ProviderID: "openai",
			Model:      "gpt-4o-mini",
		},
	}
}

func loadProvidersState() (providersState, error) {
	st := defaultProvidersState()
	b, err := os.ReadFile(providersPath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return defaultProvidersState(), nil
	}
	if st.Providers == nil {
		st.Providers = map[string]ProviderInfo{}
	}
	// 旧版 providers.json 无 ollama 时补一条默认项，便于 Ollama 与本机 API 对齐（不覆盖用户已有 ollama 配置）。
	if _, ok := st.Providers["ollama"]; !ok {
		o := defaultOllamaProvider()
		st.Providers["ollama"] = o
	}
	return st, nil
}

func saveProvidersState(st providersState) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(providersPath(), b, 0o644)
}

func providersAsList(st providersState) []ProviderInfo {
	out := make([]ProviderInfo, 0, len(st.Providers))
	for _, v := range st.Providers {
		out = append(out, v)
	}
	return out
}

func (pc *ProvidersController) ListAllProviders(c *gin.Context) {
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, providersAsList(st))
}

func (pc *ProvidersController) ConfigureProvider(c *gin.Context) {
	providerID := c.Param("provider_id")
	var body ProviderConfigRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider '" + providerID + "' not found"})
		return
	}
	if body.APIKey != nil {
		p.APIKey = *body.APIKey
	}
	if body.BaseURL != nil {
		p.BaseURL = *body.BaseURL
	}
	if body.ChatModel != nil {
		p.ChatModel = *body.ChatModel
	}
	if body.GenerateKw != nil {
		p.GenerateKw = body.GenerateKw
	}
	st.Providers[providerID] = p
	if err := saveProvidersState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (pc *ProvidersController) CreateCustomProvider(c *gin.Context) {
	var body CreateCustomProviderRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if body.ID == "" || body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id and name are required"})
		return
	}
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, exists := st.Providers[body.ID]; exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider already exists"})
		return
	}
	p := ProviderInfo{
		ID:           body.ID,
		Name:         body.Name,
		BaseURL:      body.DefaultBaseURL,
		APIKeyPrefix: body.APIKeyPrefix,
		ChatModel:    body.ChatModel,
		Models:       body.Models,
		Custom:       true,
	}
	st.Providers[body.ID] = p
	if err := saveProvidersState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (pc *ProvidersController) TestProvider(c *gin.Context) {
	providerID := c.Param("provider_id")
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider '" + providerID + "' not found"})
		return
	}
	if providerID == "ollama" {
		if _, err := ollamaListModelsFromDaemon(c.Request.Context()); err != nil {
			c.JSON(http.StatusOK, TestConnectionResponse{
				Success: false,
				Message: "Connection failed: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, TestConnectionResponse{
			Success: true,
			Message: "Connection successful",
		})
		return
	}
	if providerIsAnthropic(p) {
		if _, err := fetchAnthropicModels(c.Request.Context(), p); err != nil {
			c.JSON(http.StatusOK, TestConnectionResponse{
				Success: false,
				Message: "Connection failed: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, TestConnectionResponse{
			Success: true,
			Message: "Connection successful",
		})
		return
	}
	if providerUsesOpenAICompat(p) {
		if _, err := fetchOpenAICompatModels(c.Request.Context(), p); err != nil {
			c.JSON(http.StatusOK, TestConnectionResponse{
				Success: false,
				Message: "Connection failed: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, TestConnectionResponse{
			Success: true,
			Message: "Connection successful",
		})
		return
	}
	success := strings.TrimSpace(p.BaseURL) != ""
	msg := "Connection successful"
	if !success {
		msg = "Connection failed: provider base_url is empty"
	}
	c.JSON(http.StatusOK, TestConnectionResponse{Success: success, Message: msg})
}

func (pc *ProvidersController) DiscoverModels(c *gin.Context) {
	providerID := c.Param("provider_id")
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider '" + providerID + "' not found"})
		return
	}
	if providerID == "ollama" {
		items, err := ollamaListModelsFromDaemon(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusOK, DiscoverModelsResponse{
				Success: false,
				Models:  p.Models,
				Message: "discover failed: " + err.Error(),
			})
			return
		}
		existing := make(map[string]ModelInfo, len(p.Models))
		for _, m := range p.Models {
			existing[m.ID] = m
		}
		added := 0
		for _, om := range items {
			id := strings.TrimSpace(om.Name)
			if id == "" {
				continue
			}
			if _, ok := existing[id]; !ok {
				added++
			}
			existing[id] = ModelInfo{ID: id, Name: id}
		}
		next := make([]ModelInfo, 0, len(existing))
		for _, m := range existing {
			next = append(next, m)
		}
		p.Models = next
		st.Providers[providerID] = p
		if err := saveProvidersState(st); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, DiscoverModelsResponse{
			Success:    true,
			Models:     p.Models,
			AddedCount: added,
			Message:    "Discovered models from Ollama daemon",
		})
		return
	}
	if providerIsAnthropic(p) {
		models, err := fetchAnthropicModels(c.Request.Context(), p)
		if err != nil {
			c.JSON(http.StatusOK, DiscoverModelsResponse{
				Success: false,
				Models:  p.Models,
				Message: "discover failed: " + err.Error(),
			})
			return
		}
		added := mergeDiscoveredModels(&p, models)
		st.Providers[providerID] = p
		if err := saveProvidersState(st); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, DiscoverModelsResponse{
			Success:    true,
			Models:     p.Models,
			AddedCount: added,
			Message:    "Discovered models from Anthropic API",
		})
		return
	}
	if providerUsesOpenAICompat(p) {
		models, err := fetchOpenAICompatModels(c.Request.Context(), p)
		if err != nil {
			c.JSON(http.StatusOK, DiscoverModelsResponse{
				Success: false,
				Models:  p.Models,
				Message: "discover failed: " + err.Error(),
			})
			return
		}
		added := mergeDiscoveredModels(&p, models)
		st.Providers[providerID] = p
		if err := saveProvidersState(st); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, DiscoverModelsResponse{
			Success:    true,
			Models:     p.Models,
			AddedCount: added,
			Message:    "Discovered models from provider API",
		})
		return
	}
	c.JSON(http.StatusOK, DiscoverModelsResponse{
		Success: true,
		Models:  p.Models,
	})
}

func mergeDiscoveredModels(p *ProviderInfo, discovered []ModelInfo) int {
	existing := make(map[string]ModelInfo, len(p.Models))
	for _, m := range p.Models {
		existing[m.ID] = m
	}
	added := 0
	for _, m := range discovered {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		if _, ok := existing[m.ID]; !ok {
			added++
		}
		existing[m.ID] = m
	}
	next := make([]ModelInfo, 0, len(existing))
	for _, m := range existing {
		next = append(next, m)
	}
	p.Models = next
	return added
}

func modelsURL(base string) string {
	b := strings.TrimSpace(strings.TrimSuffix(base, "/"))
	if b == "" {
		return ""
	}
	if strings.HasSuffix(b, "/v1") {
		return b + "/models"
	}
	return b + "/v1/models"
}

func fetchOpenAICompatModels(ctx context.Context, p ProviderInfo) ([]ModelInfo, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return nil, fmt.Errorf("provider base_url is empty")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, fmt.Errorf("provider api_key is empty")
	}
	url := modelsURL(p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("models %d: %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
	}
	var obj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return nil, err
	}
	data, _ := obj["data"].([]any)
	out := make([]ModelInfo, 0, len(data))
	for _, it := range data {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, ModelInfo{ID: id, Name: id})
	}
	return out, nil
}

func fetchAnthropicModels(ctx context.Context, p ProviderInfo) ([]ModelInfo, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return nil, fmt.Errorf("provider base_url is empty")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, fmt.Errorf("provider api_key is empty")
	}
	url := modelsURL(p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, fmt.Errorf("models %d: %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
	}
	var obj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return nil, err
	}
	data, _ := obj["data"].([]any)
	out := make([]ModelInfo, 0, len(data))
	for _, it := range data {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, ModelInfo{ID: id, Name: id})
	}
	return out, nil
}

func (pc *ProvidersController) TestModel(c *gin.Context) {
	providerID := c.Param("provider_id")
	var body struct {
		ModelID string `json:"model_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider '" + providerID + "' not found"})
		return
	}
	for _, m := range p.Models {
		if m.ID == body.ModelID {
			c.JSON(http.StatusOK, TestConnectionResponse{
				Success: true,
				Message: "Model connection successful",
			})
			return
		}
	}
	c.JSON(http.StatusOK, TestConnectionResponse{
		Success: false,
		Message: "Model connection failed: model not found",
	})
}

func (pc *ProvidersController) DeleteCustomProvider(c *gin.Context) {
	providerID := c.Param("provider_id")
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok || !p.Custom {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Custom Provider '" + providerID + "' not found"})
		return
	}
	delete(st.Providers, providerID)
	if st.Active.ProviderID == providerID {
		st.Active = defaultProvidersState().Active
	}
	if err := saveProvidersState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, providersAsList(st))
}

func (pc *ProvidersController) AddModel(c *gin.Context) {
	providerID := c.Param("provider_id")
	var body AddModelRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider '" + providerID + "' not found"})
		return
	}
	for _, m := range p.Models {
		if m.ID == body.ID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Model '" + body.ID + "' already exists"})
			return
		}
	}
	p.Models = append(p.Models, ModelInfo{ID: body.ID, Name: body.Name})
	st.Providers[providerID] = p
	if err := saveProvidersState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (pc *ProvidersController) RemoveModel(c *gin.Context) {
	providerID := c.Param("provider_id")
	modelID := c.Param("model_id")
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[providerID]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provider '" + providerID + "' not found"})
		return
	}
	next := make([]ModelInfo, 0, len(p.Models))
	removed := false
	for _, m := range p.Models {
		if m.ID == modelID {
			removed = true
			continue
		}
		next = append(next, m)
	}
	if !removed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model '" + modelID + "' not found"})
		return
	}
	p.Models = next
	st.Providers[providerID] = p
	if st.Active.ProviderID == providerID && st.Active.Model == modelID {
		st.Active.Model = ""
	}
	if err := saveProvidersState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (pc *ProvidersController) GetActiveModels(c *gin.Context) {
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ActiveModelsInfo{ActiveLLM: st.Active})
}

func (pc *ProvidersController) SetActiveModel(c *gin.Context) {
	var body ModelSlotRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	st, err := loadProvidersState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	p, ok := st.Providers[body.ProviderID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Provider '" + body.ProviderID + "' not found"})
		return
	}
	found := false
	for _, m := range p.Models {
		if m.ID == body.Model {
			found = true
			break
		}
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model '" + body.Model + "' not found in provider"})
		return
	}
	st.Active = ActiveModel{ProviderID: body.ProviderID, Model: body.Model}
	if err := saveProvidersState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ActiveModelsInfo{ActiveLLM: st.Active})
}
