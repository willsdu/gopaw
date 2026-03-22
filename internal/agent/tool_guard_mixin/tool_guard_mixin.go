package tool_guard_mixin

// ToolGuardMixin 对应 copaw/agents/tool_guard_mixin.py：
// - 当模型尝试调用“敏感工具”时，生成等待审批的挂起请求；
// - 由用户通过 approve/deny 消息做出决策；
// - 决策结果影响工具是否继续执行。
//
// 当前 gopaw 还没有把 tool-guard 迁移到 Agent 层，
// 因此这里仅提供骨架类型与方法约定。
type ToolGuardMixin struct{}

// ToolGuardState 表示一次工具调用的审批状态。
type ToolGuardState struct {
	RequestID string
	ToolName  string
	// Pending 表示是否仍在等待用户审批。
	Pending bool
}

