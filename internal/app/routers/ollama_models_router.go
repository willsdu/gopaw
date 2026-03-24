package routers

// /api/ollama-models/*：列表/拉取/删除走 Ollama 守护进程 HTTP API；下载任务仍写入 local_models_state.json 供状态轮询。

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type OllamaDownloadRequest struct {
	Name string `json:"name"`
}

type OllamaModelResponse struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

type OllamaDownloadTaskResponse struct {
	TaskID string               `json:"task_id"`
	Status string               `json:"status"`
	Name   string               `json:"name"`
	Error  string               `json:"error,omitempty"`
	Result *OllamaModelResponse `json:"result,omitempty"`
}

type OllamaModelsController struct{}

func (oc *OllamaModelsController) ListOllamaModels(c *gin.Context) {
	models, err := ollamaListModelsFromDaemon(c.Request.Context())
	if err != nil {
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "connection refused") ||
			strings.Contains(strings.ToLower(msg), "no connection") ||
			strings.Contains(strings.ToLower(msg), "failed to connect") {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to list Ollama models: " + msg + " (is Ollama running? set OLLAMA_HOST or providers.json ollama base_url)",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list Ollama models: " + msg})
		return
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	c.JSON(http.StatusOK, models)
}

func (oc *OllamaModelsController) DownloadOllamaModel(c *gin.Context) {
	var body OllamaDownloadRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	taskID := time.Now().UTC().Format("20060102150405.000000000")
	task := DownloadTaskResponse{
		TaskID:  taskID,
		Status:  "pending",
		RepoID:  body.Name,
		Backend: "ollama",
		Source:  "ollama",
	}
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	for id, t := range st.Tasks {
		if t.Backend == "ollama" && (t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled") {
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

	go runOllamaPullWorker(ctx, body.Name, taskID)

	c.JSON(http.StatusOK, OllamaDownloadTaskResponse{
		TaskID: task.TaskID,
		Status: task.Status,
		Name:   body.Name,
	})
}

func runOllamaPullWorker(ctx context.Context, modelName, taskID string) {
	defer func() {
		localMu.Lock()
		delete(localCancelFuncs, taskID)
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
		t := cur.Tasks[taskID]
		t.Status = "downloading"
		cur.Tasks[taskID] = t
	})

	err := ollamaPullStream(ctx, modelName)
	if ctx.Err() != nil {
		update(func(cur *localModelsState) {
			t := cur.Tasks[taskID]
			t.Status = "cancelled"
			cur.Tasks[taskID] = t
		})
		notifyConsoleIfConfigured("Ollama model pull cancelled: %s", modelName)
		return
	}
	if err != nil {
		update(func(cur *localModelsState) {
			t := cur.Tasks[taskID]
			t.Status = "failed"
			t.Error = err.Error()
			cur.Tasks[taskID] = t
		})
		notifyConsoleIfConfigured("Ollama model pull failed (%s): %s", modelName, err.Error())
		return
	}

	var meta OllamaModelResponse
	models, listErr := ollamaListModelsFromDaemon(context.Background())
	if listErr == nil {
		for _, m := range models {
			if m.Name == modelName {
				meta = m
				break
			}
		}
	}
	model := LocalModelResponse{
		ID:          modelName,
		RepoID:      modelName,
		Filename:    "",
		Backend:     "ollama",
		Source:      "ollama",
		FileSize:    meta.Size,
		LocalPath:   "",
		DisplayName: modelName,
	}
	update(func(cur *localModelsState) {
		cur.Models[modelName] = model
		t := cur.Tasks[taskID]
		t.Status = "completed"
		t.Result = &model
		cur.Tasks[taskID] = t
	})
	notifyConsoleIfConfigured("Ollama model pull completed: %s", modelName)
}

func (oc *OllamaModelsController) GetOllamaDownloadStatus(c *gin.Context) {
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := []OllamaDownloadTaskResponse{}
	for _, t := range st.Tasks {
		if t.Backend != "ollama" {
			continue
		}
		var result *OllamaModelResponse
		if t.Result != nil {
			result = &OllamaModelResponse{
				Name:       t.Result.ID,
				Size:       t.Result.FileSize,
				Digest:     "",
				ModifiedAt: "",
			}
		}
		out = append(out, OllamaDownloadTaskResponse{
			TaskID: t.TaskID,
			Status: t.Status,
			Name:   t.RepoID,
			Error:  t.Error,
			Result: result,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	c.JSON(http.StatusOK, out)
}

func (oc *OllamaModelsController) CancelOllamaDownload(c *gin.Context) {
	taskID := c.Param("task_id")
	localMu.Lock()
	cancelFn, ok := localCancelFuncs[taskID]
	localMu.Unlock()
	if ok && cancelFn != nil {
		cancelFn()
	}
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	t, exists := st.Tasks[taskID]
	if !exists || t.Backend != "ollama" {
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

func (oc *OllamaModelsController) DeleteOllamaModel(c *gin.Context) {
	name := c.Param("name")
	if err := ollamaDeleteOnDaemon(c.Request.Context(), name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "deleted", "name": name})
		return
	}
	delete(st.Models, name)
	_ = saveLocalState(st)
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "name": name})
}
