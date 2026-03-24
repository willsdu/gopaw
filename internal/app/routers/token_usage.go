package routers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/willisdu/gopaw/internal/config"
)

// 本文件实现 GET /api/token-usage，读取与 copaw 相同的 token_usage.json 结构并聚合统计。
// 磁盘格式：按日期分桶，再按 "provider_id:model_name" 存明细（见 copaw/token_usage/manager.py）。

// TokenUsageStats 与 Python TokenUsageStats 字段一致（按 provider / 按日期的聚合块）。
type TokenUsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CallCount        int `json:"call_count"`
}

// TokenUsageByModel 与 Python TokenUsageByModel 一致（by_model 的值类型）。
type TokenUsageByModel struct {
	ProviderID       string `json:"provider_id"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CallCount        int    `json:"call_count"`
}

// TokenUsageSummary 与 Python TokenUsageSummary 一致，供控制台与工具消费。
type TokenUsageSummary struct {
	TotalPromptTokens     int                          `json:"total_prompt_tokens"`
	TotalCompletionTokens int                          `json:"total_completion_tokens"`
	TotalCalls            int                          `json:"total_calls"`
	ByModel               map[string]TokenUsageByModel `json:"by_model"`
	ByProvider            map[string]TokenUsageStats   `json:"by_provider"`
	ByDate                map[string]TokenUsageStats   `json:"by_date"`
}

type TokenUsageController struct{}

func tokenUsageFilePath() string {
	return filepath.Join(config.WorkingDir(), "token_usage.json")
}

// loadTokenUsageRaw 读取 token_usage.json：{ "YYYY-MM-DD": { "composite_key": { ... } } }。
func loadTokenUsageRaw() (map[string]map[string]map[string]any, error) {
	path := tokenUsageFilePath()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]map[string]any{}, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return map[string]map[string]map[string]any{}, nil
	}
	var root map[string]map[string]map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return map[string]map[string]map[string]any{}, nil
	}
	if root == nil {
		return map[string]map[string]map[string]any{}, nil
	}
	return root, nil
}

func parseISODateLocal(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// GetTokenUsage 对应 Python get_token_usage：支持 start_date、end_date、model、provider 查询参数。
func (tc *TokenUsageController) GetTokenUsage(c *gin.Context) {
	end := time.Now()
	if t, ok := parseISODateLocal(c.Query("end_date")); ok {
		end = t
	}
	start := end.AddDate(0, 0, -30)
	if t, ok := parseISODateLocal(c.Query("start_date")); ok {
		start = t
	}
	if start.After(end) {
		start, end = end, start
	}

	modelFilter := c.Query("model")
	providerFilter := c.Query("provider")

	data, err := loadTokenUsageRaw()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	summary := TokenUsageSummary{
		ByModel:    map[string]TokenUsageByModel{},
		ByProvider: map[string]TokenUsageStats{},
		ByDate:     map[string]TokenUsageStats{},
	}

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		byKey, ok := data[dateStr]
		if !ok {
			continue
		}
		for _, entry := range byKey {
			recModel := stringField(entry, "model_name")
			if recModel == "" {
				recModel = stringField(entry, "model")
			}
			recProvider := stringField(entry, "provider_id")
			if modelFilter != "" && recModel != modelFilter {
				continue
			}
			if providerFilter != "" && recProvider != providerFilter {
				continue
			}
			pt := intField(entry, "prompt_tokens")
			ct := intField(entry, "completion_tokens")
			cc := intField(entry, "call_count")

			summary.TotalPromptTokens += pt
			summary.TotalCompletionTokens += ct
			summary.TotalCalls += cc

			composite := recModel
			if recProvider != "" {
				composite = recProvider + ":" + recModel
			}
			bm := summary.ByModel[composite]
			bm.ProviderID = recProvider
			bm.Model = recModel
			bm.PromptTokens += pt
			bm.CompletionTokens += ct
			bm.CallCount += cc
			summary.ByModel[composite] = bm

			bp := summary.ByProvider[recProvider]
			bp.PromptTokens += pt
			bp.CompletionTokens += ct
			bp.CallCount += cc
			summary.ByProvider[recProvider] = bp

			bd := summary.ByDate[dateStr]
			bd.PromptTokens += pt
			bd.CompletionTokens += ct
			bd.CallCount += cc
			summary.ByDate[dateStr] = bd
		}
	}

	// by_date 按日期键排序输出（Python 使用 sorted(by_date_raw.items())，JSON 对象顺序在 Go 中按插入序）。
	summary.ByDate = sortByDateMap(summary.ByDate)

	c.JSON(http.StatusOK, summary)
}

func stringField(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func intField(m map[string]any, k string) int {
	v, ok := m[k]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return 0
	}
}

func sortByDateMap(in map[string]TokenUsageStats) map[string]TokenUsageStats {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]TokenUsageStats, len(keys))
	for _, k := range keys {
		out[k] = in[k]
	}
	return out
}

var tokenUsageWriteMu sync.Mutex

// RecordTokenUsage 追加一条调用统计，与 copaw token_usage.TokenUsageManager.record 写入格式一致（按本地日期分桶）。
func RecordTokenUsage(providerID, modelName string, promptTokens, completionTokens int) {
	if modelName == "" {
		modelName = "unknown"
	}
	dateStr := time.Now().In(time.Local).Format("2006-01-02")
	composite := providerID + ":" + modelName

	tokenUsageWriteMu.Lock()
	defer tokenUsageWriteMu.Unlock()

	data, err := loadTokenUsageRaw()
	if err != nil {
		return
	}
	if data == nil {
		data = map[string]map[string]map[string]any{}
	}
	byKey, ok := data[dateStr]
	if !ok || byKey == nil {
		byKey = map[string]map[string]any{}
		data[dateStr] = byKey
	}
	ent := byKey[composite]
	if ent == nil {
		ent = map[string]any{
			"provider_id":       providerID,
			"model_name":        modelName,
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"call_count":        1,
		}
	} else {
		ent["provider_id"] = providerID
		ent["model_name"] = modelName
		ent["prompt_tokens"] = intField(ent, "prompt_tokens") + promptTokens
		ent["completion_tokens"] = intField(ent, "completion_tokens") + completionTokens
		ent["call_count"] = intField(ent, "call_count") + 1
	}
	byKey[composite] = ent
	_ = atomicWriteJSON(tokenUsageFilePath(), data)
}
