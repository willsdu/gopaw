package routers

// 本文件实现 /api/agent/*：工作区下 agent/working、agent/memory 目录中的 .md 文件列表与读写，
// 以及 agent/config.json 中的语言、运行参数（max_iters 等）、system_prompt_files 配置。
// 路径与安全策略与 copaw/app/routers/agent.py 对齐（禁止路径穿越，仅允许 *.md 文件名）。

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/willisdu/gopaw/internal/config"
)

type MdFileInfo struct {
	Filename     string `json:"filename"`
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	CreatedTime  string `json:"created_time"`
	ModifiedTime string `json:"modified_time"`
}

type MdFileContent struct {
	Content string `json:"content"`
}

// AgentsRunningConfig 与 copaw AgentsRunningConfig / 控制台类型对齐。
type AgentsRunningConfig struct {
	MaxIters                 int     `json:"max_iters"`
	MaxInputLength           int     `json:"max_input_length"`
	MemoryCompactRatio       float64 `json:"memory_compact_ratio"`
	MemoryReserveRatio       float64 `json:"memory_reserve_ratio"`
	EnableToolResultCompact  bool    `json:"enable_tool_result_compact"`
	ToolResultCompactKeepN   int     `json:"tool_result_compact_keep_n"`
}

type agentConfigFile struct {
	Language          string              `json:"language"`
	Running           AgentsRunningConfig `json:"running"`
	SystemPromptFiles []string            `json:"system_prompt_files"`
}

func defaultAgentConfig() agentConfigFile {
	return agentConfigFile{
		Language: "en",
		Running: AgentsRunningConfig{
			MaxIters:                 50,
			MaxInputLength:           131072,
			MemoryCompactRatio:     0.75,
			MemoryReserveRatio:     0.1,
			EnableToolResultCompact: false,
			ToolResultCompactKeepN:  5,
		},
		SystemPromptFiles: []string{
			"AGENTS.md",
			"SOUL.md",
			"PROFILE.md",
		},
	}
}

func agentBaseDir() string {
	return filepath.Join(config.WorkingDir(), "agent")
}

func workingMDDir() string {
	return filepath.Join(agentBaseDir(), "working")
}

func memoryMDDir() string {
	return filepath.Join(agentBaseDir(), "memory")
}

func agentConfigPath() string {
	return filepath.Join(agentBaseDir(), "config.json")
}

func ensureAgentDirs() error {
	for _, dir := range []string{agentBaseDir(), workingMDDir(), memoryMDDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeMDName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("md_name is required")
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", errors.New("invalid md_name")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		return "", errors.New("md_name must end with .md")
	}
	return name, nil
}

func listMDInfos(dir string) ([]MdFileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []MdFileInfo{}, nil
		}
		return nil, err
	}
	out := make([]MdFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime().UTC().Format(time.RFC3339)
		out = append(out, MdFileInfo{
			Filename:     name,
			Path:         full,
			Size:         info.Size(),
			CreatedTime:  mod,
			ModifiedTime: mod,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out, nil
}

func readMDFile(dir, mdName string) (string, error) {
	safe, err := sanitizeMDName(mdName)
	if err != nil {
		return "", err
	}
	full := filepath.Join(dir, safe)
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func writeMDFile(dir, mdName, content string) error {
	safe, err := sanitizeMDName(mdName)
	if err != nil {
		return err
	}
	full := filepath.Join(dir, safe)
	return os.WriteFile(full, []byte(content), 0o644)
}

func loadAgentConfig() (agentConfigFile, error) {
	cfg := defaultAgentConfig()
	path := agentConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return defaultAgentConfig(), nil
	}
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	return cfg, nil
}

func saveAgentConfig(cfg agentConfigFile) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(agentConfigPath(), b, 0o644)
}

func runningDefaultsAsMap() map[string]any {
	cfg := defaultAgentConfig()
	b, err := json.Marshal(cfg.Running)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func loadAgentRunningRawFromDisk() map[string]any {
	b, err := os.ReadFile(agentConfigPath())
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	raw, _ := root["running"].(map[string]any)
	return raw
}

// effectiveAgentsRunningMap：copaw 默认 → config.json agents.running → agent/config.json running（后者覆盖前者）。
func effectiveAgentsRunningMap() map[string]any {
	m := mergeShallowJSON(runningDefaultsAsMap(), loadAgentsRunningFromMain())
	return mergeShallowJSON(m, loadAgentRunningRawFromDisk())
}

// agent/config.json 中若存在 system_prompt_files 键则视为覆盖层（含空数组）。
func loadAgentSystemPromptFilesRawFromDisk() ([]string, bool) {
	b, err := os.ReadFile(agentConfigPath())
	if err != nil {
		return nil, false
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil, false
	}
	v, ok := root["system_prompt_files"]
	if !ok {
		return nil, false
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, true
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out, true
}

func effectiveAgentLanguage() string {
	cfg, err := loadAgentConfig()
	fileLang := ""
	if err == nil {
		fileLang = strings.TrimSpace(cfg.Language)
	}
	if fileLang != "" {
		return strings.ToLower(fileLang)
	}
	if s := strings.TrimSpace(loadAgentsLanguageFromMain()); s != "" {
		return strings.ToLower(s)
	}
	if err == nil && cfg.Language != "" {
		return strings.ToLower(cfg.Language)
	}
	return "en"
}

func effectiveSystemPromptFiles() []string {
	def := defaultAgentConfig().SystemPromptFiles
	mainList := loadSystemPromptFilesFromMain()
	base := def
	if len(mainList) > 0 {
		base = append([]string(nil), mainList...)
	}
	if disk, ok := loadAgentSystemPromptFilesRawFromDisk(); ok {
		return disk
	}
	return base
}

type AgentController struct{}

func (a *AgentController) ListWorkingFiles(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	files, err := listMDInfos(workingMDDir())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, files)
}

func (a *AgentController) ReadWorkingFile(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	content, err := readMDFile(workingMDDir(), c.Param("md_name"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, MdFileContent{Content: content})
}

func (a *AgentController) WriteWorkingFile(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var req MdFileContent
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if err := writeMDFile(workingMDDir(), c.Param("md_name"), req.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"written": true})
}

func (a *AgentController) ListMemoryFiles(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	files, err := listMDInfos(memoryMDDir())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, files)
}

func (a *AgentController) ReadMemoryFile(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	content, err := readMDFile(memoryMDDir(), c.Param("md_name"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, MdFileContent{Content: content})
}

func (a *AgentController) WriteMemoryFile(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var req MdFileContent
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if err := writeMDFile(memoryMDDir(), c.Param("md_name"), req.Content); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"written": true})
}

func (a *AgentController) GetAgentLanguage(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"language": effectiveAgentLanguage()})
}

func (a *AgentController) PutAgentLanguage(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var body map[string]string
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	language := strings.ToLower(strings.TrimSpace(body["language"]))
	valid := map[string]struct{}{"zh": {}, "en": {}, "ru": {}}
	if _, ok := valid[language]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid language, must be one of: en, ru, zh",
		})
		return
	}
	cfg, err := loadAgentConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cfg.Language = language
	if err := saveAgentConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"language":     language,
		"copied_files": []string{},
	})
}

func (a *AgentController) GetAgentsRunningConfig(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveAgentsRunningMap())
}

func (a *AgentController) PutAgentsRunningConfig(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	final := mergeShallowJSON(effectiveAgentsRunningMap(), body)
	payload, err := json.Marshal(final)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cfg, err := loadAgentConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := json.Unmarshal(payload, &cfg.Running); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid running config: " + err.Error()})
		return
	}
	if err := saveAgentConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveAgentsRunningMap())
}

func (a *AgentController) GetSystemPromptFiles(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveSystemPromptFiles())
}

func (a *AgentController) PutSystemPromptFiles(c *gin.Context) {
	if err := ensureAgentDirs(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var files []string
	if err := c.ShouldBindJSON(&files); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	cfg, err := loadAgentConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cfg.SystemPromptFiles = files
	if err := saveAgentConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, effectiveSystemPromptFiles())
}
