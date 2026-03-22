package memory

// MemoryManager 负责会话/Agent 的持久化记忆（对应 copaw/agents/memory/memory_manager.py）。
//
// 在 copaw 中，这部分通常把 agent 的 memory state 写入文件/Redis，
// 并在后续对话加载回来。
//
// 当前为骨架：仅定义方法约定，便于未来迁移接入。
type MemoryManager interface {
	// Start/Close 用于管理资源生命周期（可选）。
	Start() error
	Close() error
}

