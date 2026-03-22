package tools

// Tool 对应 copaw/agents/tools 的概念：可被 agent 调用的外部能力。
//
// 当前 gopaw 的工具执行尚未迁移到该目录，因此这里只给出骨架接口。
type Tool interface {
	// Name 返回工具名称，用于路由/注册。
	Name() string
	// Call 执行工具。
	Call(input any) (output any, err error)
}

