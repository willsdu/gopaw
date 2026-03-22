package hooks

// MemoryCompaction 对应 copaw/agents/hooks/memory_compaction.py。
// 它通常在记忆增长过快时对历史进行压缩/归纳，减少上下文长度。
//
// 当前为骨架占位，待后续迁移真实逻辑。
type MemoryCompaction interface {
	Compact() error
}

