package routers

import (
	"fmt"
	"strings"

	"github.com/willisdu/gopaw/internal/app/channels"
)

// maybeConsolePushAfterProcess 在对话成功后，若请求带 channel=console，则推送到 consolepush（与 copaw ConsoleChannel 行为对齐）。
func maybeConsolePushAfterProcess(raw map[string]any, sessionKey, assistantText string) {
	if raw == nil {
		return
	}
	ch := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw["channel"])))
	if ch != "console" {
		return
	}
	uid := strings.TrimSpace(fmt.Sprint(raw["user_id"]))
	channels.DispatchText("console", sessionKey, uid, assistantText, false)
}
