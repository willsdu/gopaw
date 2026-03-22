package skills

// SkillsManager 对应 copaw/agents/skills_manager.py 的角色：
// - 维护可用技能集合（active/builtin/customized）；
// - 在启用/禁用技能后更新缓存或重新构建执行所需的技能注册表。
//
// 当前 gopaw 的技能管理主要通过 internal/app/skills/service.go 暴露 HTTP 层接口。
// 该骨架用于在未来把“Agent 层技能注册”迁移/对齐到 internal/agent。
type SkillsManager interface {
	// Reload 重新加载技能配置（从目录/存储同步到内存结构）。
	Reload() error

	// EnabledSkills 返回当前启用技能名称列表。
	EnabledSkills() ([]string, error)
}

