package routers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type TokenUsageSummary struct {
	StartDate   string         `json:"start_date"`
	EndDate     string         `json:"end_date"`
	Model       string         `json:"model,omitempty"`
	Provider    string         `json:"provider,omitempty"`
	TotalInput  int64          `json:"total_input_tokens"`
	TotalOutput int64          `json:"total_output_tokens"`
	Items       map[string]any `json:"items"`
}

type TokenUsageController struct{}

func parseISODate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func (tc *TokenUsageController) GetTokenUsage(c *gin.Context) {
	end := time.Now()
	if t, ok := parseISODate(c.Query("end_date")); ok {
		end = t
	}
	start := end.AddDate(0, 0, -30)
	if t, ok := parseISODate(c.Query("start_date")); ok {
		start = t
	}
	if start.After(end) {
		start, end = end, start
	}
	c.JSON(http.StatusOK, TokenUsageSummary{
		StartDate:   start.Format("2006-01-02"),
		EndDate:     end.Format("2006-01-02"),
		Model:       c.Query("model"),
		Provider:    c.Query("provider"),
		TotalInput:  0,
		TotalOutput: 0,
		Items:       map[string]any{},
	})
}
