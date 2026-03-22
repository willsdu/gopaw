package routing_chat_model

// RoutingChatModel 对应 copaw/agents/routing_chat_model.py 的角色：
// 根据上下文/配置把请求路由到合适的模型（例如不同 provider、不同上下文策略）。
//
// 这里仅提供骨架类型，供后续迁移时对齐接口。
type RoutingChatModel interface {
	// Chat 执行一次聊天/推理调用。
	Chat(input any) (output any, err error)
}

