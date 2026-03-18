package routers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// SkillService 定义了 skills 相关的核心能力接口。
// 这里是根据 copaw/src/copaw/app/agents/skills_manager.py 中的用法抽象出来的，
// 具体实现可以放在 internal/app/ 下的其他包中，然后注入进来。
type SkillService interface {
	ListAllSkills(ctx context.Context) ([]SkillInfo, error)
	ListAvailableSkills(ctx context.Context) ([]SkillInfo, error)
	DisableSkill(ctx context.Context, name string) (bool, error)
	EnableSkill(ctx context.Context, name string) (bool, error)
	CreateSkill(ctx context.Context, req CreateSkillRequest) (bool, error)
	DeleteSkill(ctx context.Context, name string) (bool, error)
	LoadSkillFile(ctx context.Context, skillName, source, filePath string) (string, error)
}

// SkillInfo 是对 Python 版 SkillInfo 的简化映射。
// 后续可以根据实际字段再扩展。
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// 其他字段可按需补充
}

// SkillSpec = SkillInfo + enabled
type SkillSpec struct {
	SkillInfo
	Enabled bool `json:"enabled"`
}

// CreateSkillRequest 对应 Python 版 CreateSkillRequest。
type CreateSkillRequest struct {
	Name       string                 `json:"name"`
	Content    string                 `json:"content"`
	References map[string]any        `json:"references,omitempty"`
	Scripts    map[string]any        `json:"scripts,omitempty"`
}

// HubSkillSpec 对应 Python 版 HubSkillSpec。
type HubSkillSpec struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
}

// HubInstallRequest 对应 Python 版 HubInstallRequest。
type HubInstallRequest struct {
	BundleURL string `json:"bundle_url"`
	Version   string `json:"version,omitempty"`
	Enable    bool   `json:"enable"`
	Overwrite bool   `json:"overwrite"`
}

// HubService 抽象 skills hub 的能力（search / install）。
type HubService interface {
	Search(ctx context.Context, q string, limit int) ([]HubSkillSpec, error)
	Install(ctx context.Context, req HubInstallRequest) (result HubInstallResult, err error)
}

// HubInstallResult 是 HubService.Install 的精简结果。
type HubInstallResult struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	SourceURL string `json:"source_url"`
}

// RegisterSkillRoutes 使用 gin 将 /skills 开头的 HTTP 路由注册到路由组上。
// 建议调用方传入类似 router.Group("/api") 之类的分组，再由本函数在其下挂载 /skills。
func RegisterSkillRoutes(group *gin.RouterGroup, skills SkillService, hub HubService) {
	skillsGroup := group.Group("/skills")

	// GET /skills
	skillsGroup.GET("", func(c *gin.Context) {
		ctx := c.Request.Context()
		all, err := skills.ListAllSkills(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		available, err := skills.ListAvailableSkills(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		enabledSet := make(map[string]struct{}, len(available))
		for _, s := range available {
			enabledSet[s.Name] = struct{}{}
		}
		var out []SkillSpec
		for _, s := range all {
			_, enabled := enabledSet[s.Name]
			out = append(out, SkillSpec{
				SkillInfo: s,
				Enabled:   enabled,
			})
		}
		c.JSON(http.StatusOK, out)
	})

	// POST /skills
	skillsGroup.POST("", func(c *gin.Context) {
		ctx := c.Request.Context()
		var req CreateSkillRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}
		created, err := skills.CreateSkill(ctx, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"created": created})
	})

	// GET /skills/available
	skillsGroup.GET("/available", func(c *gin.Context) {
		ctx := c.Request.Context()
		available, err := skills.ListAvailableSkills(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var out []SkillSpec
		for _, s := range available {
			out = append(out, SkillSpec{
				SkillInfo: s,
				Enabled:   true,
			})
		}
		c.JSON(http.StatusOK, out)
	})

	// GET /skills/hub/search?q=...&limit=...
	skillsGroup.GET("/hub/search", func(c *gin.Context) {
		if hub == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "hub service not configured"})
			return
		}
		ctx := c.Request.Context()
		q := c.DefaultQuery("q", "")
		limitStr := c.DefaultQuery("limit", "20")
		limit := 20
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
		results, err := hub.Search(ctx, q, limit)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, results)
	})

	// POST /skills/hub/install
	skillsGroup.POST("/hub/install", func(c *gin.Context) {
		if hub == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "hub service not configured"})
			return
		}
		ctx := c.Request.Context()
		var req HubInstallRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}
		result, err := hub.Install(ctx, req)
		if err != nil {
			var badReq *BadRequestError
			var upstream *UpstreamError
			switch {
			case errors.As(err, &badReq):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			case errors.As(err, &upstream):
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusBadGateway, gin.H{"error": "skill hub import failed: " + err.Error()})
			}
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"installed":  true,
			"name":       result.Name,
			"enabled":    result.Enabled,
			"source_url": result.SourceURL,
		})
	})

	// POST /skills/batch-disable
	skillsGroup.POST("/batch-disable", func(c *gin.Context) {
		ctx := c.Request.Context()
		var names []string
		if err := c.ShouldBindJSON(&names); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}
		for _, name := range names {
			if _, err := skills.DisableSkill(ctx, name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.Status(http.StatusNoContent)
	})

	// POST /skills/batch-enable
	skillsGroup.POST("/batch-enable", func(c *gin.Context) {
		ctx := c.Request.Context()
		var names []string
		if err := c.ShouldBindJSON(&names); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
			return
		}
		for _, name := range names {
			if _, err := skills.EnableSkill(ctx, name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
		c.Status(http.StatusNoContent)
	})

	// POST /skills/{skill_name}/enable
	skillsGroup.POST("/:skill_name/enable", func(c *gin.Context) {
		ctx := c.Request.Context()
		skillName := c.Param("skill_name")
		enabled, err := skills.EnableSkill(ctx, skillName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"enabled": enabled})
	})

	// POST /skills/{skill_name}/disable
	skillsGroup.POST("/:skill_name/disable", func(c *gin.Context) {
		ctx := c.Request.Context()
		skillName := c.Param("skill_name")
		disabled, err := skills.DisableSkill(ctx, skillName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"disabled": disabled})
	})

	// DELETE /skills/{skill_name}
	skillsGroup.DELETE("/:skill_name", func(c *gin.Context) {
		ctx := c.Request.Context()
		skillName := c.Param("skill_name")
		deleted, err := skills.DeleteSkill(ctx, skillName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": deleted})
	})

	// GET /skills/{skill_name}/files/{source}/{file_path...}
	skillsGroup.GET("/:skill_name/files/:source/*filePath", func(c *gin.Context) {
		ctx := c.Request.Context()
		skillName := c.Param("skill_name")
		source := c.Param("source")
		filePath := strings.TrimPrefix(c.Param("filePath"), "/")
		content, err := skills.LoadSkillFile(ctx, skillName, source, filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"content": content})
	})
}

// BadRequestError 和 UpstreamError 是为了方便 HubService.Install
// 映射到合适的 HTTP 状态码而预留的错误类型。
type BadRequestError struct {
	Msg string
}

func (e *BadRequestError) Error() string { return e.Msg }

type UpstreamError struct {
	Msg string
}

func (e *UpstreamError) Error() string { return e.Msg }


