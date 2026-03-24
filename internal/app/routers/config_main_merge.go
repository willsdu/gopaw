package routers

// 从工作区 config.json（copaw 主配置）读取片段，与 app_config.json 同名字段浅合并（app 覆盖主配置）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"gopaw/internal/config"
)

func loadRawMainConfigJSON() map[string]any {
	p := filepath.Join(config.WorkingDir(), "config.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	return root
}

func descendToMap(root map[string]any, keys ...string) map[string]any {
	if root == nil {
		return nil
	}
	cur := root
	for _, key := range keys {
		nxt, ok := cur[key].(map[string]any)
		if !ok {
			return nil
		}
		cur = nxt
	}
	return cur
}

func mergeShallowJSON(base, overlay map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

func loadHeartbeatFromMainAgentsDefaults() map[string]any {
	return descendToMap(loadRawMainConfigJSON(), "agents", "defaults", "heartbeat")
}

func loadLLMRoutingFromMainAgents() map[string]any {
	return descendToMap(loadRawMainConfigJSON(), "agents", "llm_routing")
}

// loadAgentsRunningFromMain 读取 config.json → agents.running（与 copaw AgentsRunningConfig 同源）。
func loadAgentsRunningFromMain() map[string]any {
	return descendToMap(loadRawMainConfigJSON(), "agents", "running")
}

// MainConfigTopField 读取 config.json 顶层键（如 last_api、last_dispatch），供控制台与 copaw 共用工作区时展示。
func MainConfigTopField(key string) any {
	root := loadRawMainConfigJSON()
	if root == nil {
		return nil
	}
	return root[key]
}

func loadAgentsLanguageFromMain() string {
	m := descendToMap(loadRawMainConfigJSON(), "agents")
	if m == nil {
		return ""
	}
	s, _ := m["language"].(string)
	return s
}

func loadSystemPromptFilesFromMain() []string {
	m := descendToMap(loadRawMainConfigJSON(), "agents")
	if m == nil {
		return nil
	}
	raw, ok := m["system_prompt_files"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// effectiveHeartbeatForAPI 合并 config.json agents.defaults.heartbeat 与 app_config heartbeat。
func effectiveHeartbeatForAPI(app map[string]any) map[string]any {
	base := loadHeartbeatFromMainAgentsDefaults()
	merged := mergeShallowJSON(base, app)
	if len(merged) == 0 {
		return cloneMapShallow(defaultAppConfigState().Heartbeat)
	}
	return merged
}

// effectiveLLMRoutingForAPI 合并 config.json agents.llm_routing 与 app_config llm_routing。
func effectiveLLMRoutingForAPI(app map[string]any) map[string]any {
	base := loadLLMRoutingFromMainAgents()
	return mergeShallowJSON(base, app)
}
