// Package routers 注册与 copaw FastAPI 侧一致的 HTTP API（前缀 /api），
// 包括 agent、配置、工具、MCP、模型、技能、定时任务、会话等模块。
// 数据优先落地到 config.WorkingDir() 下的 JSON / .env 文件，便于与 Python 版共用工作区。
package routers

import (
	"github.com/gin-gonic/gin"

	"gopaw/internal/version"
)

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
	// 与 copaw 控制台 rootApi.readRoot 对齐：GET /api/
	apiGroup.GET("", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"name":      "gopaw",
			"status":    "ok",
			"framework": "gopaw",
		})
	})
	apiGroup.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"name":      "gopaw",
			"status":    "ok",
			"framework": "gopaw",
		})
	})
	// 与 copaw FastAPI GET /api/version 对齐，便于控制台探测后端
	// 与 copaw GET /api/version 对齐；版本号可由构建时 -ldflags 注入 internal/version。
	apiGroup.GET("/version", func(c *gin.Context) {
		c.JSON(200, gin.H{"version": version.Version})
	})

	// agent 域：/api/agent
	agentGroup := apiGroup.Group("/agent")
	// AgentScope Runtime 兼容：process / health / shutdown（供控制台 @agentscope-ai/chat）
	agentAppCtrl := &AgentAppController{}
	agentGroup.GET("", agentAppCtrl.Root)
	agentGroup.GET("/", agentAppCtrl.Root)
	agentGroup.GET("/health", agentAppCtrl.Health)
	agentGroup.POST("/shutdown", agentAppCtrl.Shutdown)
	agentGroup.GET("/admin/status", agentAppCtrl.AdminStatus)
	agentGroup.POST("/admin/shutdown", agentAppCtrl.AdminShutdown)
	agentGroup.POST("/process", agentAppCtrl.Process)

	agentCtrl := &AgentController{}
	agentGroup.GET("/files", agentCtrl.ListWorkingFiles)
	agentGroup.GET("/files/:md_name", agentCtrl.ReadWorkingFile)
	agentGroup.PUT("/files/:md_name", agentCtrl.WriteWorkingFile)
	agentGroup.GET("/memory", agentCtrl.ListMemoryFiles)
	agentGroup.GET("/memory/:md_name", agentCtrl.ReadMemoryFile)
	agentGroup.PUT("/memory/:md_name", agentCtrl.WriteMemoryFile)
	agentGroup.GET("/language", agentCtrl.GetAgentLanguage)
	agentGroup.PUT("/language", agentCtrl.PutAgentLanguage)
	agentGroup.GET("/running-config", agentCtrl.GetAgentsRunningConfig)
	agentGroup.PUT("/running-config", agentCtrl.PutAgentsRunningConfig)
	agentGroup.GET("/system-prompt-files", agentCtrl.GetSystemPromptFiles)
	agentGroup.PUT("/system-prompt-files", agentCtrl.PutSystemPromptFiles)

	// console 域：/api/console
	consoleGroup := apiGroup.Group("/console")
	consoleCtrl := &ConsoleController{}
	consoleGroup.GET("/push-messages", consoleCtrl.GetPushMessages)

	// envs 域：/api/envs
	envsGroup := apiGroup.Group("/envs")
	envsCtrl := &EnvsController{}
	envsGroup.GET("", envsCtrl.ListEnvs)
	envsGroup.PUT("", envsCtrl.BatchSaveEnvs)
	envsGroup.DELETE("/:key", envsCtrl.DeleteEnv)

	// workspace 域：/api/workspace
	workspaceGroup := apiGroup.Group("/workspace")
	workspaceCtrl := &WorkspaceController{}
	workspaceGroup.GET("/download", workspaceCtrl.DownloadWorkspace)
	workspaceGroup.POST("/upload", workspaceCtrl.UploadWorkspace)

	// token-usage 域：/api/token-usage
	tokenGroup := apiGroup.Group("/token-usage")
	tokenCtrl := &TokenUsageController{}
	tokenGroup.GET("", tokenCtrl.GetTokenUsage)

	// cron 域：/api/cron — 对应 copaw/app/crons/api.py（jobs.json + 本地 runtime 状态）
	cronGroup := apiGroup.Group("/cron")
	cronCtrl := &CronController{}
	cronGroup.GET("/jobs", cronCtrl.ListCronJobs)
	cronGroup.GET("/jobs/:job_id", cronCtrl.GetCronJob)
	cronGroup.POST("/jobs", cronCtrl.CreateCronJob)
	cronGroup.PUT("/jobs/:job_id", cronCtrl.ReplaceCronJob)
	cronGroup.DELETE("/jobs/:job_id", cronCtrl.DeleteCronJob)
	cronGroup.POST("/jobs/:job_id/pause", cronCtrl.PauseCronJob)
	cronGroup.POST("/jobs/:job_id/resume", cronCtrl.ResumeCronJob)
	cronGroup.POST("/jobs/:job_id/run", cronCtrl.RunCronJob)
	cronGroup.GET("/jobs/:job_id/state", cronCtrl.GetCronJobState)

	// chats 域：/api/chats — 对应 copaw/app/runner/api.py（chats.json；消息体在 gopaw 中为空）
	chatsGroup := apiGroup.Group("/chats")
	chatsCtrl := &ChatsController{}
	chatsGroup.GET("", chatsCtrl.ListChats)
	chatsGroup.POST("", chatsCtrl.CreateChat)
	chatsGroup.POST("/batch-delete", chatsCtrl.BatchDeleteChats)
	chatsGroup.GET("/:chat_id", chatsCtrl.GetChat)
	chatsGroup.PUT("/:chat_id", chatsCtrl.UpdateChat)
	chatsGroup.DELETE("/:chat_id", chatsCtrl.DeleteChat)

	// config 域：/api/config
	cfgGroup := apiGroup.Group("/config")
	cfgCtrl := &ConfigController{}
	cfgGroup.GET("", cfgCtrl.GetAll)
	cfgGroup.GET("/heartbeat", cfgCtrl.GetHeartbeat)
	cfgGroup.PUT("/heartbeat", cfgCtrl.PutHeartbeat)
	cfgGroup.GET("/channels", cfgCtrl.GetChannels)
	cfgGroup.PUT("/channels", cfgCtrl.PutChannels)
	cfgGroup.GET("/channels/available", cfgCtrl.GetAvailableChannels)
	cfgGroup.GET("/channels/types", cfgCtrl.GetChannelTypes)
	cfgGroup.GET("/channels/:channel_name", cfgCtrl.GetChannelByName)
	cfgGroup.PUT("/channels/:channel_name", cfgCtrl.PutChannelByName)
	cfgGroup.GET("/security/tool-guard/builtin-rules", cfgCtrl.GetBuiltinToolGuardRules)
	cfgGroup.GET("/security/tool-guard", cfgCtrl.GetSecurityToolGuard)
	cfgGroup.PUT("/security/tool-guard", cfgCtrl.PutSecurityToolGuard)
	cfgGroup.GET("/llm-routing", cfgCtrl.GetLLMRouting)
	cfgGroup.PUT("/llm-routing", cfgCtrl.PutLLMRouting)
	cfgGroup.GET("/console", cfgCtrl.GetConsole)
	cfgGroup.PUT("/console", cfgCtrl.PutConsole)
	cfgGroup.GET("/tool-guard", cfgCtrl.GetToolGuard)
	cfgGroup.PUT("/tool-guard", cfgCtrl.PutToolGuard)

	// tools 域：/api/tools
	toolsGroup := apiGroup.Group("/tools")
	toolsCtrl := &ToolsController{}
	toolsGroup.GET("", toolsCtrl.ListTools)
	toolsGroup.PATCH("/:tool_name/toggle", toolsCtrl.ToggleTool)

	// mcp 域：/api/mcp
	mcpGroup := apiGroup.Group("/mcp")
	mcpCtrl := &MCPController{}
	mcpGroup.GET("", mcpCtrl.List)
	mcpGroup.GET("/:client_key", mcpCtrl.Get)
	mcpGroup.POST("", mcpCtrl.Create)
	mcpGroup.PUT("/:client_key", mcpCtrl.Update)
	mcpGroup.PATCH("/:client_key/toggle", mcpCtrl.Toggle)
	mcpGroup.DELETE("/:client_key", mcpCtrl.Delete)

	// models/providers 域：/api/models
	providersGroup := apiGroup.Group("/models")
	providersCtrl := &ProvidersController{}
	providersGroup.GET("", providersCtrl.ListAllProviders)
	providersGroup.PUT("/:provider_id/config", providersCtrl.ConfigureProvider)
	providersGroup.POST("/custom-providers", providersCtrl.CreateCustomProvider)
	providersGroup.POST("/:provider_id/test", providersCtrl.TestProvider)
	providersGroup.POST("/:provider_id/discover", providersCtrl.DiscoverModels)
	providersGroup.POST("/:provider_id/models/test", providersCtrl.TestModel)
	providersGroup.DELETE("/custom-providers/:provider_id", providersCtrl.DeleteCustomProvider)
	providersGroup.POST("/:provider_id/models", providersCtrl.AddModel)
	providersGroup.DELETE("/:provider_id/models/:model_id", providersCtrl.RemoveModel)
	providersGroup.GET("/active", providersCtrl.GetActiveModels)
	providersGroup.PUT("/active", providersCtrl.SetActiveModel)

	// local-models 域：/api/local-models
	localModelsGroup := apiGroup.Group("/local-models")
	localCtrl := &LocalModelsController{}
	localModelsGroup.GET("", localCtrl.ListLocal)
	localModelsGroup.POST("/download", localCtrl.DownloadModel)
	localModelsGroup.GET("/download-status", localCtrl.GetDownloadStatus)
	localModelsGroup.DELETE("/:model_id", localCtrl.DeleteLocal)
	localModelsGroup.POST("/cancel-download/:task_id", localCtrl.CancelDownload)

	// ollama-models 域：/api/ollama-models
	ollamaGroup := apiGroup.Group("/ollama-models")
	ollamaCtrl := &OllamaModelsController{}
	ollamaGroup.GET("", ollamaCtrl.ListOllamaModels)
	ollamaGroup.POST("/download", ollamaCtrl.DownloadOllamaModel)
	ollamaGroup.GET("/download-status", ollamaCtrl.GetOllamaDownloadStatus)
	ollamaGroup.DELETE("/download/:task_id", ollamaCtrl.CancelOllamaDownload)
	ollamaGroup.DELETE("/:name", ollamaCtrl.DeleteOllamaModel)

	// skills_stream 域：/api/skills/ai/optimize/stream
	streamCtrl := &SkillsStreamController{}
	apiGroup.POST("/skills/ai/optimize/stream", streamCtrl.OptimizeSkillStream)

	// voice 域（root level）
	voiceCtrl := &VoiceController{}
	engine.POST("/voice/incoming", voiceCtrl.VoiceIncoming)
	engine.GET("/voice/ws", voiceCtrl.VoiceWS)
	engine.POST("/voice/status-callback", voiceCtrl.VoiceStatusCallback)

	// 其它同级模块（先占位，路径对齐 copaw）

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
