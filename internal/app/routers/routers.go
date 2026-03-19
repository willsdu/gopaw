package routers

import "github.com/gin-gonic/gin"

// Router 作为所有 HTTP 路由的集中注册入口。
// 通过依赖注入（注入 SkillService/HubService）把业务能力挂到 gin 引擎上。
type Router struct {
	skillService SkillService
	hubService   HubService
}

// NewRouter 创建路由注册器。
// - skillService：技能管理能力（list/enable/disable/create/delete/read 等）
// - hubService：技能 Hub 的能力（search/install 等）
func NewRouter(skillService SkillService, hubService HubService) *Router {
	return &Router{
		skillService: skillService,
		hubService:   hubService,
	}
}

// Register 将所有 HTTP 路由注册到 gin.Engine。
func (r *Router) Register(engine *gin.Engine) {
	// 统一 API 前缀：/api
	apiGroup := engine.Group("/api")
	// skills 域：/api/skills
	skillsGroup := apiGroup.Group("/skills")

	// 用控制器承载各个 endpoint 的 handler 逻辑。
	ctrl := &SkillController{
		skills: r.skillService,
		hub:    r.hubService,
	}

	// 技能列表
	// GET /api/skills
	skillsGroup.GET("", ctrl.ListAllSkills)

	// 技能创建/导入
	// POST /api/skills
	skillsGroup.POST("", ctrl.CreateSkill)

	// 技能可用列表（Enabled=true 的集合由返回侧决定）
	// GET /api/skills/available
	skillsGroup.GET("/available", ctrl.ListAvailableSkills)

	// skills hub 搜索
	// GET /api/skills/hub/search?q=...&limit=...
	skillsGroup.GET("/hub/search", ctrl.HubSearch)

	// skills hub 安装
	// POST /api/skills/hub/install
	skillsGroup.POST("/hub/install", ctrl.HubInstall)

	// 批量禁用
	// POST /api/skills/batch-disable
	skillsGroup.POST("/batch-disable", ctrl.BatchDisable)

	// 批量启用
	// POST /api/skills/batch-enable
	skillsGroup.POST("/batch-enable", ctrl.BatchEnable)

	// 单个技能启用
	// POST /api/skills/{skill_name}/enable
	skillsGroup.POST("/:skill_name/enable", ctrl.EnableSkillByName)

	// 单个技能禁用
	// POST /api/skills/{skill_name}/disable
	skillsGroup.POST("/:skill_name/disable", ctrl.DisableSkillByName)

	// 删除技能
	// DELETE /api/skills/{skill_name}
	skillsGroup.DELETE("/:skill_name", ctrl.DeleteSkillByName)

	// 读取技能文件内容
	// GET /api/skills/{skill_name}/files/{source}/{file_path...}
	skillsGroup.GET("/:skill_name/files/:source/*filePath", ctrl.LoadSkillFileByPath)
}
