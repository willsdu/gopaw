package hooks

// Bootstrap 对应 copaw/agents/hooks/bootstrap.py：
// - 在系统启动/Agent 初始化时构建必要的组件（模型、记忆、技能集合等）；
// - 或在运行前做初始化检查。
//
// 目前 gopaw 尚未接入完整 Agent 推理链路，因此这里只提供骨架占位，
// 便于后续按模块迁移。
type Bootstrap interface {
	// Init 执行初始化动作。
	Init() error
}

