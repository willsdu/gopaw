package routers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"gopaw/internal/config"
)

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

	cancelled := make(chan struct{})
	localMu.Lock()
	localCancelFuncs[taskID] = func() { close(cancelled) }
	localMu.Unlock()

	go func(req DownloadRequest, tID string) {
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

		select {
		case <-cancelled:
			update(func(cur *localModelsState) {
				t := cur.Tasks[tID]
				t.Status = "cancelled"
				cur.Tasks[tID] = t
			})
			return
		case <-time.After(1200 * time.Millisecond):
		}

		m := LocalModelResponse{
			ID:          localModelID(req),
			RepoID:      req.RepoID,
			Filename:    req.Filename,
			Backend:     req.Backend,
			Source:      req.Source,
			FileSize:    0,
			LocalPath:   filepath.Join(config.WorkingDir(), "models", localModelID(req)),
			DisplayName: localModelID(req),
		}
		update(func(cur *localModelsState) {
			cur.Models[m.ID] = m
			t := cur.Tasks[tID]
			t.Status = "completed"
			t.Result = &m
			cur.Tasks[tID] = t
		})

		localMu.Lock()
		delete(localCancelFuncs, tID)
		localMu.Unlock()
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
