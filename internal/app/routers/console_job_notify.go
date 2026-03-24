package routers

import (
	"fmt"
	"os"
	"strings"

	"gopaw/internal/app/channels"
)

// 若设置 GOPAW_CONSOLE_PUSH_SESSION，则将任务类消息推到该 session 的 console 队列（与 cron 推送共用机制）。
const envConsolePushSession = "GOPAW_CONSOLE_PUSH_SESSION"

func notifyConsoleIfConfigured(format string, args ...any) {
	sid := strings.TrimSpace(os.Getenv(envConsolePushSession))
	if sid == "" {
		return
	}
	msg := strings.TrimSpace(fmt.Sprintf(format, args...))
	if msg == "" {
		return
	}
	channels.DispatchText("console", normSessionKey(sid), "", msg, false)
}
