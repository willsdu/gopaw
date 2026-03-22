package runner

// 这个包对应 copaw/src/copaw/app/runner，
// 用于承载「会话 / 任务运行器」相关逻辑（如 session、manager、models 等）。
//
// 建议后续在这里定义：
//   - 会话/任务的领域模型
//   - 运行器接口及实现
//   - 持久化仓库接口（可在 internal/app/runner/repo 下建子包）
//
// 同时，当前我已经在 `internal/agent` 下按 copaw/agents 的分层创建了“Agent 层骨架”
//（command_handler / hooks / memory / prompt / react_agent / routing_chat_model / skills / tool_guard）。
// 后续你迁移推理链路/命令/审批时，可以让 internal/app/runner 去組装并调用这些 internal/agent 包。

