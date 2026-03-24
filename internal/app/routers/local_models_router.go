package routers

// 本文件实现 /api/local-models/*：与 copaw 路由形状一致；校验 backend/source 枚举。
// - source=huggingface：若存在 huggingface-cli，执行真实 download。
// - source=modelscope：若存在 python/python3 且已 pip install modelscope，用 snapshot_download 拉全库（与 copaw MLX 路径类似）。
// - 否则短时占位完成。

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

func isAllowedLocalBackend(b string) bool {
	switch strings.ToLower(strings.TrimSpace(b)) {
	case "llamacpp", "mlx":
		return true
	default:
		return false
	}
}

func isAllowedLocalSource(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "huggingface", "modelscope":
		return true
	default:
		return false
	}
}

func findHuggingfaceCLI() string {
	for _, name := range []string{"huggingface-cli", "huggingface-cli.exe"} {
		p, err := exec.LookPath(name)
		if err == nil {
			return p
		}
	}
	return ""
}

func findPythonCLI() string {
	for _, name := range []string{"python3", "python", "py"} {
		p, err := exec.LookPath(name)
		if err == nil {
			return p
		}
	}
	return ""
}

// modelscopeSnapshotScript：与 copaw 使用 modelscope.hub.snapshot_download 一致；local_dir / model_id 经 argv 传入，避免注入。
const modelscopeSnapshotScript = `import sys
local_dir = sys.argv[1]
model_id = sys.argv[2]
try:
    from modelscope.hub.snapshot_download import snapshot_download as _sd
except ImportError:
    from modelscope import snapshot_download as _sd
_sd(model_id=model_id, local_dir=local_dir)
`

func safeModelDirSegment(id string) string {
	s := strings.ReplaceAll(id, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

func dirTotalSize(root string) int64 {
	var n int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		n += info.Size()
		return nil
	})
	return n
}

type DownloadRequest struct {
	RepoID   string `json:"repo_id"`
	Filename string `json:"filename,omitempty"`
	Backend  string `json:"backend"`
	Source   string `json:"source"`
}

type LocalModelResponse struct {
	ID          string `json:"id"`
	RepoID      string `json:"repo_id"`
	Filename    string `json:"filename"`
	Backend     string `json:"backend"`
	Source      string `json:"source"`
	FileSize    int64  `json:"file_size"`
	LocalPath   string `json:"local_path"`
	DisplayName string `json:"display_name"`
}

type DownloadTaskResponse struct {
	TaskID   string              `json:"task_id"`
	Status   string              `json:"status"`
	RepoID   string              `json:"repo_id"`
	Filename string              `json:"filename,omitempty"`
	Backend  string              `json:"backend"`
	Source   string              `json:"source"`
	Error    string              `json:"error,omitempty"`
	Result   *LocalModelResponse `json:"result,omitempty"`
}

type localModelsState struct {
	Models map[string]LocalModelResponse   `json:"models"`
	Tasks  map[string]DownloadTaskResponse `json:"tasks"`
}

type LocalModelsController struct{}

var (
	localMu          sync.Mutex
	localCancelFuncs = map[string]func(){}
)

func localModelsPath() string {
	return filepath.Join(config.WorkingDir(), "local_models_state.json")
}

func loadLocalState() (localModelsState, error) {
	st := localModelsState{
		Models: map[string]LocalModelResponse{},
		Tasks:  map[string]DownloadTaskResponse{},
	}
	b, err := os.ReadFile(localModelsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return localModelsState{Models: map[string]LocalModelResponse{}, Tasks: map[string]DownloadTaskResponse{}}, nil
	}
	if st.Models == nil {
		st.Models = map[string]LocalModelResponse{}
	}
	if st.Tasks == nil {
		st.Tasks = map[string]DownloadTaskResponse{}
	}
	return st, nil
}

func saveLocalState(st localModelsState) error {
	if err := os.MkdirAll(config.WorkingDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(localModelsPath(), b, 0o644)
}

func localModelID(req DownloadRequest) string {
	if req.Filename != "" {
		return req.RepoID + ":" + req.Filename
	}
	return req.RepoID
}

func (lc *LocalModelsController) ListLocal(c *gin.Context) {
	backend := c.Query("backend")
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]LocalModelResponse, 0, len(st.Models))
	for _, m := range st.Models {
		if backend != "" && m.Backend != backend {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	c.JSON(http.StatusOK, out)
}

func (lc *LocalModelsController) DownloadModel(c *gin.Context) {
	var body DownloadRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if body.RepoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "repo_id is required"})
		return
	}
	if body.Backend == "" {
		body.Backend = "llamacpp"
	}
	if body.Source == "" {
		body.Source = "huggingface"
	}
	if !isAllowedLocalBackend(body.Backend) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid backend: use llamacpp or mlx"})
		return
	}
	if !isAllowedLocalSource(body.Source) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid source: use huggingface or modelscope"})
		return
	}
	taskID := time.Now().UTC().Format("20060102150405.000000000")
	task := DownloadTaskResponse{
		TaskID:   taskID,
		Status:   "pending",
		RepoID:   body.RepoID,
		Filename: body.Filename,
		Backend:  body.Backend,
		Source:   body.Source,
	}
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for id, t := range st.Tasks {
		if t.Backend == body.Backend && (t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled") {
			delete(st.Tasks, id)
		}
	}
	st.Tasks[taskID] = task
	if err := saveLocalState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	localMu.Lock()
	localCancelFuncs[taskID] = cancel
	localMu.Unlock()

	go func(req DownloadRequest, tID string) {
		defer func() {
			localMu.Lock()
			delete(localCancelFuncs, tID)
			localMu.Unlock()
		}()

		update := func(fn func(*localModelsState)) {
			localMu.Lock()
			defer localMu.Unlock()
			cur, err := loadLocalState()
			if err != nil {
				return
			}
			fn(&cur)
			_ = saveLocalState(cur)
		}
		update(func(cur *localModelsState) {
			t := cur.Tasks[tID]
			t.Status = "downloading"
			cur.Tasks[tID] = t
		})

		mid := localModelID(req)
		destDir := filepath.Join(config.WorkingDir(), "models", safeModelDirSegment(mid))

		if hf := findHuggingfaceCLI(); hf != "" && strings.EqualFold(req.Source, "huggingface") {
			_ = os.MkdirAll(destDir, 0o755)
			args := []string{"download", req.RepoID, "--local-dir", destDir}
			if fn := strings.TrimSpace(req.Filename); fn != "" {
				args = append(args, "--include", fn)
			}
			cmd := exec.CommandContext(ctx, hf, args...)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				if ctx.Err() != nil {
				update(func(cur *localModelsState) {
					t := cur.Tasks[tID]
					t.Status = "cancelled"
					cur.Tasks[tID] = t
				})
					notifyConsoleIfConfigured("Local model download cancelled: %s (%s)", req.RepoID, req.Source)
					return
				}
				msg := err.Error()
				if stderr.Len() > 0 {
					msg = strings.TrimSpace(stderr.String())
				}
				update(func(cur *localModelsState) {
					t := cur.Tasks[tID]
					t.Status = "failed"
					t.Error = msg
					cur.Tasks[tID] = t
				})
				notifyConsoleIfConfigured("Local model download failed (%s / %s): %s", req.RepoID, req.Source, msg)
				return
			}
			sz := dirTotalSize(destDir)
			m := LocalModelResponse{
				ID:          mid,
				RepoID:      req.RepoID,
				Filename:    req.Filename,
				Backend:     req.Backend,
				Source:      req.Source,
				FileSize:    sz,
				LocalPath:   destDir,
				DisplayName: mid,
			}
			update(func(cur *localModelsState) {
				cur.Models[m.ID] = m
				t := cur.Tasks[tID]
				t.Status = "completed"
				t.Result = &m
				cur.Tasks[tID] = t
			})
			notifyConsoleIfConfigured("Local model download completed: %s (%s, %s)", req.RepoID, req.Backend, req.Source)
			return
		}

		if py := findPythonCLI(); py != "" && strings.EqualFold(req.Source, "modelscope") {
			_ = os.MkdirAll(destDir, 0o755)
			cmd := exec.CommandContext(ctx, py, "-c", modelscopeSnapshotScript, destDir, req.RepoID)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				if ctx.Err() != nil {
				update(func(cur *localModelsState) {
					t := cur.Tasks[tID]
					t.Status = "cancelled"
					cur.Tasks[tID] = t
				})
					notifyConsoleIfConfigured("Local model download cancelled: %s (%s)", req.RepoID, req.Source)
					return
				}
				msg := err.Error()
				if stderr.Len() > 0 {
					msg = strings.TrimSpace(stderr.String())
				}
				update(func(cur *localModelsState) {
					t := cur.Tasks[tID]
					t.Status = "failed"
					t.Error = msg
					cur.Tasks[tID] = t
				})
				notifyConsoleIfConfigured("Local model download failed (%s / %s): %s", req.RepoID, req.Source, msg)
				return
			}
			sz := dirTotalSize(destDir)
			m := LocalModelResponse{
				ID:          mid,
				RepoID:      req.RepoID,
				Filename:    req.Filename,
				Backend:     req.Backend,
				Source:      req.Source,
				FileSize:    sz,
				LocalPath:   destDir,
				DisplayName: mid,
			}
			update(func(cur *localModelsState) {
				cur.Models[m.ID] = m
				t := cur.Tasks[tID]
				t.Status = "completed"
				t.Result = &m
				cur.Tasks[tID] = t
			})
			notifyConsoleIfConfigured("Local model download completed: %s (%s, %s)", req.RepoID, req.Backend, req.Source)
			return
		}

		timer := time.NewTimer(1200 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			update(func(cur *localModelsState) {
				t := cur.Tasks[tID]
				t.Status = "cancelled"
				cur.Tasks[tID] = t
			})
			notifyConsoleIfConfigured("Local model download cancelled: %s (%s)", req.RepoID, req.Source)
			return
		case <-timer.C:
		}

		m := LocalModelResponse{
			ID:          mid,
			RepoID:      req.RepoID,
			Filename:    req.Filename,
			Backend:     req.Backend,
			Source:      req.Source,
			FileSize:    0,
			LocalPath:   destDir,
			DisplayName: mid,
		}
		update(func(cur *localModelsState) {
			cur.Models[m.ID] = m
			t := cur.Tasks[tID]
			t.Status = "completed"
			t.Result = &m
			cur.Tasks[tID] = t
		})
		notifyConsoleIfConfigured("Local model download completed (placeholder): %s (%s)", req.RepoID, req.Source)
	}(body, taskID)

	c.JSON(http.StatusOK, task)
}

func (lc *LocalModelsController) GetDownloadStatus(c *gin.Context) {
	backend := c.Query("backend")
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]DownloadTaskResponse, 0, len(st.Tasks))
	for _, t := range st.Tasks {
		if backend != "" && t.Backend != backend {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	c.JSON(http.StatusOK, out)
}

func (lc *LocalModelsController) DeleteLocal(c *gin.Context) {
	modelID := c.Param("model_id")
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, ok := st.Models[modelID]; !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	delete(st.Models, modelID)
	if err := saveLocalState(st); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "model_id": modelID})
}

func (lc *LocalModelsController) CancelDownload(c *gin.Context) {
	taskID := c.Param("task_id")
	localMu.Lock()
	cancel, ok := localCancelFuncs[taskID]
	localMu.Unlock()
	if ok {
		cancel()
	}
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	t, exists := st.Tasks[taskID]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found or not cancellable (already completed/failed/cancelled)"})
		return
	}
	if t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found or not cancellable (already completed/failed/cancelled)"})
		return
	}
	t.Status = "cancelled"
	st.Tasks[taskID] = t
	_ = saveLocalState(st)
	c.JSON(http.StatusOK, gin.H{"status": "cancelled", "task_id": taskID})
}
