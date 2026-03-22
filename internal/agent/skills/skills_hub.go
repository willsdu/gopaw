package skills

// SkillsHub 对应 copaw/agents/skills_hub.py 的角色：
// - 提供从外部 Hub 获取技能的能力（search/install 等）。
//
// 当前 gopaw 通过 internal/app/skills 的 HTTP 服务实现 skills hub（如果配置了）。
// 该骨架用于为未来迁移/复用提供统一接口。
type SkillsHub interface {
	Search(q string, limit int) ([]any, error)
	Install(slug string, version string) (any, error)
}

