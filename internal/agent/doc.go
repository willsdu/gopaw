package agent

// Package agent 负责承载 gopaw 中“Agent 层”的结构（领域模型、路由模型、命令处理、记忆/提示词/技能/工具守卫等）。
//
// 该目录是为了“仿照 copaw/src/copaw/agents/”而创建的 Go 侧骨架：
// - 先把目录与包结构搭齐，便于后续逐步把 Python 行为迁移到 Go；
// - 当前不要求这些包在业务上被实际引用，因此实现尽量保持轻量、可编译。
//
// 当你准备开始迁移具体能力时，可以从以下常见优先级入手：
// 1) 命令处理（对应 copaw/agents/command_handler.py）
// 2) tool-guard/审批链路（对应 copaw/agents/tool_guard_mixin.py）
// 3) 记忆管理（对应 copaw/agents/memory/*）
// 4) prompt/路由模型/ReactAgent（对应 copaw/agents/prompt.py、react_agent.py、routing_chat_model.py）
// 5) skills hub/skills manager（对应 copaw/agents/skills_hub.py、skills_manager.py）

