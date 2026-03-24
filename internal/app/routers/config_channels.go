package routers

// channels 在 app_config.json 中可为对象（与 copaw 控制台一致）或旧版数组；统一解析为 map 再对外合并 isBuiltin。

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
)

func availableBuiltinChannels() []string {
	all := []string{
		"console", "telegram", "discord", "voice", "feishu", "dingtalk",
		"qq", "imessage", "mattermost", "mqtt", "matrix",
	}
	// 与 copaw 的 COPAW_ENABLED_CHANNELS 类似：允许通过环境变量收窄可用通道集合。
	// 优先 GOPAW_ENABLED_CHANNELS，兼容 COPAW_ENABLED_CHANNELS（共用工作区迁移场景）。
	raw := strings.TrimSpace(os.Getenv("GOPAW_ENABLED_CHANNELS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("COPAW_ENABLED_CHANNELS"))
	}
	if raw == "" {
		return all
	}
	enabled := map[string]struct{}{}
	for _, p := range strings.Split(raw, ",") {
		if s := strings.ToLower(strings.TrimSpace(p)); s != "" {
			enabled[s] = struct{}{}
		}
	}
	out := make([]string, 0, len(all))
	for _, k := range all {
		if _, ok := enabled[k]; ok {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

func builtinChannelSet() map[string]struct{} {
	s := make(map[string]struct{}, 16)
	for _, n := range availableBuiltinChannels() {
		s[n] = struct{}{}
	}
	return s
}

func parseChannelsBlob(raw json.RawMessage) map[string]any {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return map[string]any{}
	}
	var asMap map[string]any
	if json.Unmarshal(raw, &asMap) == nil && asMap != nil {
		return asMap
	}
	var asArr []map[string]any
	if json.Unmarshal(raw, &asArr) == nil {
		out := make(map[string]any, len(asArr))
		for _, m := range asArr {
			if m == nil {
				continue
			}
			name := channelNameFromRow(m)
			if name == "" {
				continue
			}
			out[name] = channelRowToValue(m)
		}
		return out
	}
	return map[string]any{}
}

func channelNameFromRow(m map[string]any) string {
	for _, key := range []string{"channel", "type", "id"} {
		if s, ok := m[key].(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				return strings.ToLower(s)
			}
		}
	}
	return ""
}

func channelRowToValue(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "channel" || k == "type" {
			continue
		}
		out[k] = v
	}
	return out
}

func cloneMapShallow(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func stripIsBuiltinFromChannelsMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		vm, ok := v.(map[string]any)
		if !ok {
			out[k] = v
			continue
		}
		c := cloneMapShallow(vm)
		delete(c, "isBuiltin")
		out[k] = c
	}
	return out
}

// buildChannelsAPIResponse 合并内置通道默认值与磁盘上的配置，并写入 isBuiltin（与 copaw list_channels 行为接近）。
func buildChannelsAPIResponse(stored map[string]any) map[string]any {
	builtins := availableBuiltinChannels()
	bset := builtinChannelSet()
	out := make(map[string]any, len(stored)+len(builtins))

	for _, name := range builtins {
		v, ok := stored[name]
		if !ok || v == nil {
			out[name] = map[string]any{"enabled": false, "bot_prefix": "", "isBuiltin": true}
			continue
		}
		vm, ok := v.(map[string]any)
		if !ok {
			out[name] = v
			continue
		}
		merged := cloneMapShallow(vm)
		merged["isBuiltin"] = true
		out[name] = merged
	}
	for k, v := range stored {
		if _, isB := bset[k]; isB {
			continue
		}
		vm, ok := v.(map[string]any)
		if !ok {
			out[k] = v
			continue
		}
		merged := cloneMapShallow(vm)
		merged["isBuiltin"] = false
		out[k] = merged
	}
	return out
}

func marshalChannelsMap(m map[string]any) (json.RawMessage, error) {
	if m == nil {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}
