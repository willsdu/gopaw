package config

import (
	"os"
	"path/filepath"
)

// WorkingDir 返回 gopaw 工作目录，默认 ~/.gopaw。
// 可通过环境变量 GOPAW_WORKING_DIR 覆盖。
func WorkingDir() string {
	s := os.Getenv("GOPAW_WORKING_DIR")
	if s == "" {
		home, _ := os.UserHomeDir()
		s = filepath.Join(home, ".gopaw")
	}
	abs, _ := filepath.Abs(s)
	return abs
}

// ActiveSkillsDir 返回已启用技能目录（agent 实际使用的技能）。
// 对应 copaw 的 ACTIVE_SKILLS_DIR = WORKING_DIR / "active_skills"。
func ActiveSkillsDir() string {
	return filepath.Join(WorkingDir(), "active_skills")
}

// CustomizedSkillsDir 返回用户自定义技能目录。
// 对应 copaw 的 CUSTOMIZED_SKILLS_DIR = WORKING_DIR / "customized_skills"。
func CustomizedSkillsDir() string {
	return filepath.Join(WorkingDir(), "customized_skills")
}

// BuiltinSkillsDir 返回内置技能目录（可选）。
// 可通过环境变量 GOPAW_BUILTIN_SKILLS_DIR 指定；未设置时返回空字符串表示无内置技能。
func BuiltinSkillsDir() string {
	return os.Getenv("GOPAW_BUILTIN_SKILLS_DIR")
}
