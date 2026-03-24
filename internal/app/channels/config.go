package channels

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/willisdu/gopaw/internal/config"
)

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%.0f", t)
		}
		return fmt.Sprintf("%g", t)
	case json.Number:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func truthyAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return t != 0
	default:
		return false
	}
}

func appConfigPath() string {
	return filepath.Join(config.WorkingDir(), "app_config.json")
}

func mainConfigPath() string {
	return filepath.Join(config.WorkingDir(), "config.json")
}

// ChannelConfig 返回 channels 下某一内置通道的配置 map（如 "console"、"voice"）。
func ChannelConfig(name string) map[string]any {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	if m := channelFromAppConfig(name); m != nil {
		return m
	}
	if m := channelFromMainConfig(name); m != nil {
		return m
	}
	if m := channelFromAppConfigSlice(name); m != nil {
		return m
	}
	return nil
}

func channelFromAppConfig(name string) map[string]any {
	b, err := os.ReadFile(appConfigPath())
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	chRaw, ok := root["channels"]
	if !ok {
		return nil
	}
	ch, ok := chRaw.(map[string]any)
	if !ok {
		return nil
	}
	v, ok := ch[name].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func channelFromAppConfigSlice(name string) map[string]any {
	b, err := os.ReadFile(appConfigPath())
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	chRaw, ok := root["channels"]
	if !ok {
		return nil
	}
	arr, ok := chRaw.([]any)
	if !ok {
		return nil
	}
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strFromMap(m, "channel"), name) || strings.EqualFold(strFromMap(m, "type"), name) {
			return m
		}
	}
	return nil
}

func channelFromMainConfig(name string) map[string]any {
	b, err := os.ReadFile(mainConfigPath())
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	chRaw, ok := root["channels"]
	if !ok {
		return nil
	}
	ch, ok := chRaw.(map[string]any)
	if !ok {
		return nil
	}
	v, ok := ch[name].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

// ConsoleChannelEnabled 对应 copaw ConsoleConfig：默认启用。
func ConsoleChannelEnabled() bool {
	m := ChannelConfig("console")
	if m == nil {
		return true
	}
	if _, ok := m["enabled"]; !ok {
		return true
	}
	return truthyAny(m["enabled"])
}

// ConsoleBotPrefix 控制台消息前缀（与 copaw bot_prefix 一致）。
func ConsoleBotPrefix() string {
	return strFromMap(ChannelConfig("console"), "bot_prefix")
}
