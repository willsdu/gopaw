package skills

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopaw/internal/app/routers"
	"gopaw/internal/config"
)

// Service 实现 routers.SkillService，参照 copaw 的 SkillService 逻辑：
// builtin + customized 作为“全部技能”来源，active_skills 为已启用技能；
// enable = 从 builtin/customized 同步到 active，disable = 从 active 移除，
// create = 写入 customized，delete = 从 customized 删除。
type Service struct {
	builtinDir    string // 可为空
	customizedDir string
	activeDir     string
}

// NewService 根据 config 中的目录创建 SkillService。
// builtinDir 若为空则仅使用 customized + active。
func NewService() *Service {
	builtin := config.BuiltinSkillsDir()
	return &Service{
		builtinDir:    builtin,
		customizedDir: config.CustomizedSkillsDir(),
		activeDir:     config.ActiveSkillsDir(),
	}
}

const skillMarkdown = "SKILL.md"

// listSkillsFromDir 从指定目录收集所有技能（直接子目录且含有 SKILL.md），与 copaw 一致。
func (s *Service) listSkillsFromDir(dir, source string) ([]routers.SkillInfo, error) {
	if dir == "" || !isDir(dir) {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var list []routers.SkillInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		skillMD := filepath.Join(skillDir, skillMarkdown)
		if !fileExists(skillMD) {
			continue
		}
		content, err := os.ReadFile(skillMD)
		if err != nil {
			continue
		}
		desc := parseDescriptionFromFrontmatter(string(content))
		list = append(list, routers.SkillInfo{
			Name:        e.Name(),
			Description: desc,
		})
	}
	return list, nil
}

func parseDescriptionFromFrontmatter(content string) string {
	// 简单解析：第一个 --- 与第二个 --- 之间为 frontmatter，取 description: 的值
	const delim = "---"
	idx := strings.Index(content, delim)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(delim):]
	idx2 := strings.Index(rest, delim)
	if idx2 < 0 {
		return ""
	}
	fm := rest[:idx2]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			v = strings.Trim(v, "\"'")
			return v
		}
	}
	return ""
}

func (s *Service) ListAllSkills(ctx context.Context) ([]routers.SkillInfo, error) {
	_ = ctx
	// 先尝试从 active 同步到 customized（与 copaw 一致），忽略错误
	_ = s.syncFromActiveToCustomized(nil)

	var all []routers.SkillInfo
	if s.builtinDir != "" {
		builtinList, err := s.listSkillsFromDir(s.builtinDir, "builtin")
		if err != nil {
			return nil, err
		}
		all = append(all, builtinList...)
	}
	customList, err := s.listSkillsFromDir(s.customizedDir, "customized")
	if err != nil {
		return nil, err
	}
	all = append(all, customList...)
	return dedupeSkillsByName(all), nil
}

func (s *Service) ListAvailableSkills(ctx context.Context) ([]routers.SkillInfo, error) {
	_ = ctx
	return s.listSkillsFromDir(s.activeDir, "active")
}

func dedupeSkillsByName(skills []routers.SkillInfo) []routers.SkillInfo {
	byName := make(map[string]routers.SkillInfo)
	for _, sk := range skills {
		byName[sk.Name] = sk
	}
	out := make([]routers.SkillInfo, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	return out
}

func (s *Service) DisableSkill(ctx context.Context, name string) (bool, error) {
	_ = ctx
	dir := filepath.Join(s.activeDir, name)
	if !isDir(dir) {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) EnableSkill(ctx context.Context, name string) (bool, error) {
	_ = ctx
	// 从 builtin 或 customized 同步到 active
	sourceDir := ""
	if s.builtinDir != "" && isDir(filepath.Join(s.builtinDir, name)) {
		sourceDir = filepath.Join(s.builtinDir, name)
	}
	if isDir(filepath.Join(s.customizedDir, name)) {
		sourceDir = filepath.Join(s.customizedDir, name)
	}
	if sourceDir == "" {
		return false, nil
	}
	activeSkillDir := filepath.Join(s.activeDir, name)
	if err := os.MkdirAll(s.activeDir, 0o755); err != nil {
		return false, err
	}
	if isDir(activeSkillDir) {
		if err := os.RemoveAll(activeSkillDir); err != nil {
			return false, err
		}
	}
	if err := copyDir(sourceDir, activeSkillDir); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) CreateSkill(ctx context.Context, req routers.CreateSkillRequest) (bool, error) {
	_ = ctx
	skillDir := filepath.Join(s.customizedDir, req.Name)
	if err := os.MkdirAll(s.customizedDir, 0o755); err != nil {
		return false, err
	}
	if isDir(skillDir) {
		if err := os.RemoveAll(skillDir); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return false, err
	}
	skillMD := filepath.Join(skillDir, skillMarkdown)
	if err := os.WriteFile(skillMD, []byte(req.Content), 0o644); err != nil {
		return false, err
	}
	if len(req.References) > 0 {
		refDir := filepath.Join(skillDir, "references")
		if err := os.MkdirAll(refDir, 0o755); err != nil {
			return false, err
		}
		if err := createFilesFromTree(refDir, req.References); err != nil {
			return false, err
		}
	}
	if len(req.Scripts) > 0 {
		scriptsDir := filepath.Join(skillDir, "scripts")
		if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
			return false, err
		}
		if err := createFilesFromTree(scriptsDir, req.Scripts); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (s *Service) DeleteSkill(ctx context.Context, name string) (bool, error) {
	_ = ctx
	dir := filepath.Join(s.customizedDir, name)
	if !isDir(dir) {
		return false, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) LoadSkillFile(ctx context.Context, skillName, source, filePath string) (string, error) {
	_ = ctx
	if source != "builtin" && source != "customized" {
		return "", nil
	}
	filePath = filepath.Clean(filepath.FromSlash(filepath.Join("/", filePath)))
	filePath = strings.TrimPrefix(filePath, string(filepath.Separator))
	if strings.Contains(filePath, "..") {
		return "", nil
	}
	norm := filepath.ToSlash(filePath)
	if !strings.HasPrefix(norm, "references/") && !strings.HasPrefix(norm, "scripts/") {
		return "", nil
	}
	var baseDir string
	if source == "customized" {
		baseDir = s.customizedDir
	} else {
		baseDir = s.builtinDir
	}
	if baseDir == "" {
		return "", nil
	}
	full := filepath.Join(baseDir, skillName, filePath)
	if !fileExists(full) {
		return "", nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// syncFromActiveToCustomized 将 active 中与 builtin 不同的技能同步到 customized（与 copaw 一致）。
func (s *Service) syncFromActiveToCustomized(names []string) error {
	if !isDir(s.activeDir) {
		return nil
	}
	if err := os.MkdirAll(s.customizedDir, 0o755); err != nil {
		return err
	}
	activeMap := make(map[string]string)
	_ = filepath.WalkDir(s.activeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(s.activeDir, path)
		if rel == "." {
			return nil
		}
		if strings.Contains(rel, string(filepath.Separator)) {
			return nil
		}
		if fileExists(filepath.Join(path, skillMarkdown)) {
			activeMap[rel] = path
		}
		return filepath.SkipDir
	})
	builtinMap := make(map[string]struct{})
	if s.builtinDir != "" && isDir(s.builtinDir) {
		_ = filepath.WalkDir(s.builtinDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(s.builtinDir, path)
			if rel == "." {
				return nil
			}
			if strings.Contains(rel, string(filepath.Separator)) {
				return nil
			}
			builtinMap[rel] = struct{}{}
			return filepath.SkipDir
		})
	}
	for name, activePath := range activeMap {
		if names != nil && !contains(names, name) {
			continue
		}
		if _, inBuiltin := builtinMap[name]; inBuiltin {
			// 与 builtin 完全一致的可跳过（简化：不比较内容，仅跳过 builtin 有的）
			continue
		}
		dst := filepath.Join(s.customizedDir, name)
		if isDir(dst) {
			_ = os.RemoveAll(dst)
		}
		_ = copyDir(activePath, dst)
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// createFilesFromTree 根据 map[string]any 树状结构创建文件和目录。
// 值为 string 表示文件内容，为 map[string]any 表示子目录，nil 表示空文件。
func createFilesFromTree(baseDir string, tree map[string]any) error {
	for name, val := range tree {
		p := filepath.Join(baseDir, name)
		switch v := val.(type) {
		case string:
			if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
				return err
			}
		case map[string]any:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			if err := createFilesFromTree(p, v); err != nil {
				return err
			}
		default:
			// 对应 copaw 的 None / 空文件
			if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
