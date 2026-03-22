package command_handler

// CommandHandler 负责把用户输入的“命令”（例如 /compact、/new 或其他系统命令）
// 映射为对话语义更新（生成/追加消息、更新记忆等）。
//
// 该接口用于对标 copaw/agents/command_handler.py 的角色。
// 当前为骨架实现：仅定义方法签名与约定，便于后续真正迁移逻辑。
type CommandHandler interface {
	HandleConversationCommand(query string) (messages []any, err error)
}

