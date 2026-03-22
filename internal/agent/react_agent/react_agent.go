package react_agent

// ReactAgent 对应 copaw/agents/react_agent.py：
// 常见的“反思+行动（ReAct）”风格 agent 实现，会在模型输出后触发工具调用并迭代。
//
// 当前 gopaw 仍以 skills/审批/HTTP 服务为主，推理链路未接入，因此仅提供骨架占位。
type ReactAgent interface {
	// Run 执行对话推理流程，并按需产出消息序列。
	Run(input any) (output any, err error)
}

