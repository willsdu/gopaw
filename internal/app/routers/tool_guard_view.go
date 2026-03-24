package routers

// tool_guard API 与 copaw 控制台 ToolGuardConfig 对齐；磁盘上可保留旧字段 rules。
// GET 时合并 copaw 主配置 config.json → security.tool_guard 与 app_config.json → tool_guard（后者覆盖前者）。

func loadToolGuardFromMainConfigJSON() map[string]any {
	return descendToMap(loadRawMainConfigJSON(), "security", "tool_guard")
}

func mergeToolGuardLayers(base, overlay map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// effectiveToolGuardRaw 供持久化前对比或扩展使用；合并主配置与工作区 app_config。
func effectiveToolGuardRaw(appLayer map[string]any) map[string]any {
	return mergeToolGuardLayers(loadToolGuardFromMainConfigJSON(), appLayer)
}

func effectiveToolGuardForAPI(appLayer map[string]any) map[string]any {
	return toolGuardAPIView(effectiveToolGuardRaw(appLayer))
}

func toolGuardAPIView(stored map[string]any) map[string]any {
	out := map[string]any{
		"enabled":         false,
		"guarded_tools":   nil,
		"denied_tools":    []any{},
		"custom_rules":    []any{},
		"disabled_rules":  []any{},
	}
	if stored == nil {
		return out
	}
	if v, ok := stored["enabled"]; ok {
		out["enabled"] = v
	}
	if v, ok := stored["guarded_tools"]; ok {
		out["guarded_tools"] = v
	}
	if v, ok := stored["denied_tools"]; ok {
		out["denied_tools"] = v
	}
	if v, ok := stored["custom_rules"]; ok {
		out["custom_rules"] = v
	} else if v, ok := stored["rules"]; ok {
		out["custom_rules"] = v
	}
	if v, ok := stored["disabled_rules"]; ok {
		out["disabled_rules"] = v
	}
	return out
}

func persistToolGuardFromRequest(body map[string]any) map[string]any {
	if body == nil {
		return map[string]any{
			"enabled": false, "rules": []any{}, "custom_rules": []any{},
			"denied_tools": []any{}, "disabled_rules": []any{},
		}
	}
	out := cloneMapShallow(body)
	if cr, ok := out["custom_rules"]; ok {
		out["rules"] = cr
	} else if rules, ok := out["rules"]; ok {
		out["custom_rules"] = rules
	}
	return out
}

// builtinToolGuardRulesForAPI 对齐 copaw security/tool_guard/rules/dangerous_shell_commands.yaml 中条目。
func builtinToolGuardRulesForAPI() []map[string]any {
	return []map[string]any{
		{
			"id":               "TOOL_CMD_DANGEROUS_RM",
			"tools":            []any{"execute_shell_command"},
			"params":           []any{"command"},
			"category":         "command_injection",
			"severity":         "HIGH",
			"patterns":         []any{"\\brm\\b"},
			"exclude_patterns": []any{},
			"description":      "Shell command contains 'rm' which may cause data loss",
			"remediation":      "Confirm with the user before removing files or directories",
		},
		{
			"id":               "TOOL_CMD_DANGEROUS_MV",
			"tools":            []any{"execute_shell_command"},
			"params":           []any{"command"},
			"category":         "command_injection",
			"severity":         "HIGH",
			"patterns":         []any{"\\bmv\\b"},
			"exclude_patterns": []any{},
			"description":      "Shell command contains 'mv' which may move or overwrite files unexpectedly",
			"remediation":      "Confirm with the user before moving or renaming files",
		},
	}
}
