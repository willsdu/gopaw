package channels

// 这个包对应 copaw/src/copaw/app/channels，
// 用于统一管理各种外部消息通道（如 console、telegram、feishu、discord 等）。
//
// 在 Go 版本中，建议在这里定义：
//   - 通道的抽象接口（发送消息、接收消息等）
//   - 通道注册/工厂（根据配置选择不同实现）
//   - 各种具体通道的子包（可参考 copaw 中的子目录结构）

