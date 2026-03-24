package channels

import (
	"strings"

	"github.com/willisdu/gopaw/internal/app/consolepush"
)

// DispatchText 将定时任务等产生的纯文本投递到指定通道（当前仅实现 console → consolepush）。
// userID 预留与其它通道对齐；console 推送只依赖 sessionID。
func DispatchText(channelName, sessionID, userID, text string, sticky bool) {
	_ = userID
	ch := strings.ToLower(strings.TrimSpace(channelName))
	text = strings.TrimSpace(text)
	sessionID = strings.TrimSpace(sessionID)
	if ch == "" || text == "" || sessionID == "" {
		return
	}
	switch ch {
	case "console":
		pushConsole(sessionID, text, sticky)
	default:
		// 其它通道后续再接真连接
	}
}

func pushConsole(sessionID, text string, sticky bool) {
	if !ConsoleChannelEnabled() {
		return
	}
	prefix := ConsoleBotPrefix()
	if prefix != "" {
		text = prefix + text
	}
	consolepush.Append(sessionID, text, sticky)
}
