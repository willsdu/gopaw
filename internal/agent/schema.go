package agent

// Message 是 Agent 层与“对话消息”相关的最小数据结构。
//
// 目前 gopaw 仍以 HTTP/skills/审批等为主，Agent 层尚未真正接入推理链路；
// 因此这里先提供一个“骨架类型”，方便后续逐步替换为真实消息模型。
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// ApprovalDecision 用于表示 tool-guard 审批决策。
// 具体状态机/持久化逻辑后续可迁移自 copaw/agents/tool_guard_mixin.py 相关实现。
type ApprovalDecision string

const (
	ApprovalApproved ApprovalDecision = "approved"
	ApprovalDenied   ApprovalDecision = "denied"
	ApprovalTimeout  ApprovalDecision = "timeout"
)

