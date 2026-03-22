package model_factory

// ModelFactory 对应 copaw/agents/model_factory.py 的角色。
// 在 copaw 中它会根据配置选择合适的模型实现（本地/远程、多模型路由等）。
//
// 目前 gopaw 还没有把推理链路迁移到该目录，因此这里仅提供骨架接口。
type ModelFactory interface {
	// Create 创建一个可用于对话/推理的模型实例。
	// 具体返回类型在后续迁移时再细化。
	Create(name string) (any, error)
}

