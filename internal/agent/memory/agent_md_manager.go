package memory

// AgentMDManager 对应 copaw/agents/memory/agent_md_manager.py：
// 通常用于管理/拼装与模型系统提示相关的 Markdown 文件（如 AGENTS.md、PROFILE.md 等）。
//
// 这里给出骨架接口，后续迁移真实逻辑时再填充方法。
type AgentMDManager interface {
	// Reload 重新加载 markdown 配置。
	Reload() error
	// Render 返回聚合后的内容，供 prompt/routing 使用。
	Render() (string, error)
}

