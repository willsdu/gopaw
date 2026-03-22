package prompt

// Prompt 对应 copaw/agents/prompt.py：
// - 根据 AGENTS.md / SOUL.md / PROFILE.md 等内容生成系统提示词；
// - 并把运行时上下文（当前时间、会话信息、权限等）拼到提示词前后。
//
// 这里给出骨架结构，后续可以迁移 copaw 的提示词拼装逻辑。
type Prompt interface {
	Build() (string, error)
}

