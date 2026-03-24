package channels

// 对应 copaw/src/copaw/app/channels：按通道名分发出站消息。
//
// 已实现：
//   - console：读取 channels.console 是否启用与 bot_prefix，写入 internal/app/consolepush（与 GET /api/console/push-messages 打通）。
//
// 其它通道（Telegram、飞书等）后续再接真连接。

