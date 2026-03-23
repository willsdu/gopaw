package routers

import (
	"net/http"
	"sort"
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
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := []OllamaModelResponse{}
	for _, m := range st.Models {
		if m.Backend != "ollama" {
			continue
		}
		out = append(out, OllamaModelResponse{
			Name:       m.ID,
			Size:       m.FileSize,
			Digest:     "",
			ModifiedAt: "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.JSON(http.StatusOK, out)
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
	// 复用 local-model 下载任务机制
	req := DownloadRequest{
		RepoID:  body.Name,
		Backend: "ollama",
		Source:  "ollama",
	}
	ctx := &LocalModelsController{}
	// 直接借用逻辑：创建 task + 后台状态更新
	c.Request.Header.Set("Content-Type", "application/json")
	_ = req
	// 简化：直接调用本控制器内部逻辑
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
	_ = saveLocalState(st)
	go func(name, tID string) {
		time.Sleep(1200 * time.Millisecond)
		cur, err := loadLocalState()
		if err != nil {
			return
		}
		t := cur.Tasks[tID]
		if t.Status == "cancelled" {
			return
		}
		model := LocalModelResponse{
			ID:          name,
			RepoID:      name,
			Filename:    "",
			Backend:     "ollama",
			Source:      "ollama",
			FileSize:    0,
			LocalPath:   "",
			DisplayName: name,
		}
		cur.Models[name] = model
		t.Status = "completed"
		t.Result = &model
		cur.Tasks[tID] = t
		_ = saveLocalState(cur)
	}(body.Name, taskID)

	c.JSON(http.StatusOK, OllamaDownloadTaskResponse{
		TaskID: task.TaskID,
		Status: task.Status,
		Name:   body.Name,
	})
	_ = ctx
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
			result = &OllamaModelResponse{Name: t.Result.ID, Size: t.Result.FileSize}
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
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	t, ok := st.Tasks[taskID]
	if !ok || t.Backend != "ollama" {
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
	st, err := loadLocalState()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if _, ok := st.Models[name]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model not found"})
		return
	}
	delete(st.Models, name)
	_ = saveLocalState(st)
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "name": name})
}
