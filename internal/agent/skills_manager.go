package agent

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopaw/internal/app/routers"
	"gopaw/internal/config"
)

// ManagerService 实现 routers.SkillService，负责技能管理（非 hub）。
// 对应 copaw 中 skills_manager 的目录同步/读写能力。
type ManagerService struct {
	builtinDir    string // 可为空
	customizedDir string
	activeDir     string
}

// NewManagerService 根据 config 中的目录创建技能管理服务。
func NewManagerService() *ManagerService {
	builtin := config.BuiltinSkillsDir()
	return &ManagerService{
		builtinDir:    builtin,
		customizedDir: config.CustomizedSkillsDir(),
		activeDir:     config.ActiveSkillsDir(),
	}
}

const skillMarkdown = "SKILL.md"

func (s *ManagerService) listSkillsFromDir(dir string) ([]routers.SkillInfo, error) {
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

func (s *ManagerService) ListAllSkills(ctx context.Context) ([]routers.SkillInfo, error) {
	_ = ctx
	_ = s.syncFromActiveToCustomized(nil)

	var all []routers.SkillInfo
	if s.builtinDir != "" {
		builtinList, err := s.listSkillsFromDir(s.builtinDir)
		if err != nil {
			return nil, err
		}
		all = append(all, builtinList...)
	}
	customList, err := s.listSkillsFromDir(s.customizedDir)
	if err != nil {
		return nil, err
	}
	all = append(all, customList...)
	return dedupeSkillsByName(all), nil
}

func (s *ManagerService) ListAvailableSkills(ctx context.Context) ([]routers.SkillInfo, error) {
	_ = ctx
	return s.listSkillsFromDir(s.activeDir)
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

func (s *ManagerService) DisableSkill(ctx context.Context, name string) (bool, error) {
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

func (s *ManagerService) EnableSkill(ctx context.Context, name string) (bool, error) {
	_ = ctx
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

func (s *ManagerService) CreateSkill(ctx context.Context, req routers.CreateSkillRequest) (bool, error) {
	_ = ctx
	if err := os.MkdirAll(s.customizedDir, 0o755); err != nil {
		return false, err
	}
	skillDir := filepath.Join(s.customizedDir, req.Name)
	if isDir(skillDir) && !req.Overwrite {
		return false, nil
	}
	if isDir(skillDir) && req.Overwrite {
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
	if len(req.ExtraFiles) > 0 {
		if err := createFilesFromTree(skillDir, req.ExtraFiles); err != nil {
			return false, err
		}
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

func (s *ManagerService) DeleteSkill(ctx context.Context, name string) (bool, error) {
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

func (s *ManagerService) LoadSkillFile(ctx context.Context, skillName, source, filePath string) (string, error) {
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

func (s *ManagerService) syncFromActiveToCustomized(names []string) error {
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
			if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
