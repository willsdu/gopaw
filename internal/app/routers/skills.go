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
	Name       string         `json:"name"`
	Content    string         `json:"content"`
	References map[string]any `json:"references,omitempty"`
	Scripts    map[string]any `json:"scripts,omitempty"`
	// ExtraFiles 写入技能目录根下（非 references/scripts），与 Python create_skill(extra_files=...) 一致。
	ExtraFiles map[string]any `json:"extra_files,omitempty"`
	// Overwrite 为 false 且目录已存在时创建失败；Hub 安装会显式传入。
	Overwrite bool `json:"overwrite,omitempty"`
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

// SkillController 负责把 HTTP 请求（gin）转换为对 SkillService / HubService 的调用，
// 并把结果序列化为 JSON/HTTP 状态码返回给客户端。
type SkillController struct {
	skills SkillService
	hub    HubService
}

// ListAllSkills
// GET /api/skills
// - 调用：skills.ListAllSkills + skills.ListAvailableSkills
// - 输出：SkillSpec 列表，并根据 available 集合计算 Enabled 字段
func (s *SkillController) ListAllSkills(c *gin.Context) {
	ctx := c.Request.Context()
	all, err := s.skills.ListAllSkills(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	available, err := s.skills.ListAvailableSkills(ctx)
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
}

// CreateSkill
// POST /api/skills
// - Body：CreateSkillRequest（JSON）
// - 输出：{ "created": <bool> }
func (s *SkillController) CreateSkill(c *gin.Context) {
	ctx := c.Request.Context()
	var req CreateSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	created, err := s.skills.CreateSkill(ctx, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"created": created})
}

// ListAvailableSkills
// GET /api/skills/available
// - 输出：SkillSpec 列表（其中 Enabled 固定为 true）
func (s *SkillController) ListAvailableSkills(c *gin.Context) {
	ctx := c.Request.Context()
	available, err := s.skills.ListAvailableSkills(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var out []SkillSpec
	for _, sk := range available {
		out = append(out, SkillSpec{
			SkillInfo: sk,
			Enabled:   true,
		})
	}
	c.JSON(http.StatusOK, out)
}

// HubSearch
// GET /api/skills/hub/search?q=...&limit=...
// - Query：q（搜索关键词），limit（数量，默认 20）
// - 输出：[]HubSkillSpec
func (s *SkillController) HubSearch(c *gin.Context) {
	if s.hub == nil {
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
	results, err := s.hub.Search(ctx, q, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, results)
}

// HubInstall
// POST /api/skills/hub/install
// - Body：HubInstallRequest（JSON）
// - 输出：{installed, name, enabled, source_url}
// - 错误映射：
//   - BadRequestError => 400
//   - UpstreamError => 502
func (s *SkillController) HubInstall(c *gin.Context) {
	if s.hub == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "hub service not configured"})
		return
	}
	ctx := c.Request.Context()
	var req HubInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	result, err := s.hub.Install(ctx, req)
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
}

// BatchDisable
// POST /api/skills/batch-disable
// - Body：[]string（技能名称列表）
// - 输出：204 No Content
func (s *SkillController) BatchDisable(c *gin.Context) {
	ctx := c.Request.Context()
	var names []string
	if err := c.ShouldBindJSON(&names); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	for _, name := range names {
		if _, err := s.skills.DisableSkill(ctx, name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// BatchEnable
// POST /api/skills/batch-enable
// - Body：[]string（技能名称列表）
// - 输出：204 No Content
func (s *SkillController) BatchEnable(c *gin.Context) {
	ctx := c.Request.Context()
	var names []string
	if err := c.ShouldBindJSON(&names); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	for _, name := range names {
		if _, err := s.skills.EnableSkill(ctx, name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.Status(http.StatusNoContent)
}

// EnableSkillByName
// POST /api/skills/{skill_name}/enable
// - 输出：{ "enabled": <bool> }
func (s *SkillController) EnableSkillByName(c *gin.Context) {
	ctx := c.Request.Context()
	skillName := c.Param("skill_name")
	enabled, err := s.skills.EnableSkill(ctx, skillName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": enabled})
}

// DisableSkillByName
// POST /api/skills/{skill_name}/disable
// - 输出：{ "disabled": <bool> }
func (s *SkillController) DisableSkillByName(c *gin.Context) {
	ctx := c.Request.Context()
	skillName := c.Param("skill_name")
	disabled, err := s.skills.DisableSkill(ctx, skillName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"disabled": disabled})
}

// DeleteSkillByName
// DELETE /api/skills/{skill_name}
// - 输出：{ "deleted": <bool> }
func (s *SkillController) DeleteSkillByName(c *gin.Context) {
	ctx := c.Request.Context()
	skillName := c.Param("skill_name")
	deleted, err := s.skills.DeleteSkill(ctx, skillName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": deleted})
}

// LoadSkillFileByPath
// GET /api/skills/{skill_name}/files/{source}/{file_path...}
// - 将 file_path 作为通配路径拼接后传给 skills.LoadSkillFile
// - 输出：{ "content": <string> }
func (s *SkillController) LoadSkillFileByPath(c *gin.Context) {
	ctx := c.Request.Context()
	skillName := c.Param("skill_name")
	source := c.Param("source")
	filePath := strings.TrimPrefix(c.Param("filePath"), "/")
	content, err := s.skills.LoadSkillFile(ctx, skillName, source, filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": content})
}

// BadRequestError / UpstreamError：
// 用于 HubService.Install 的错误分类，以便在 API 层映射到更合适的 HTTP 状态码。
type BadRequestError struct {
	Msg string
}

func (e *BadRequestError) Error() string { return e.Msg }

type UpstreamError struct {
	Msg string
}

func (e *UpstreamError) Error() string { return e.Msg }
