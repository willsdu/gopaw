package routers

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"gopaw/internal/app/channels"
	"gopaw/internal/config"
)

// 本文件实现 /api/cron/*，与 copaw/app/crons/api.py 路径与语义对齐。
// 定时任务定义持久化到 jobs.json（与 Python JsonJobRepository 相同文件名），便于双端共用数据。
// 说明：gopaw 未内嵌 APScheduler 周期调度；POST .../run 在后台执行一次。
// dispatch.channel == console 时，text / agent 成功结果经 channels 包写入 consolepush，供 GET /api/console/push-messages 拉取。

const (
	cronJobsFileName    = "jobs.json"
	cronRuntimeFileName = "cron_runtime.json"
)

var cronMu sync.Mutex

type jobsDocument struct {
	Version int               `json:"version"`
	Jobs    []json.RawMessage `json:"jobs"`
}

type cronRuntimeDocument struct {
	Paused map[string]bool                    `json:"paused,omitempty"`
	States map[string]map[string]any          `json:"states,omitempty"`
}

type CronController struct{}

func cronJobsPath() string {
	return filepath.Join(config.WorkingDir(), cronJobsFileName)
}

func cronRuntimePath() string {
	return filepath.Join(config.WorkingDir(), cronRuntimeFileName)
}

func atomicWriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadJobsDoc() (jobsDocument, error) {
	var doc jobsDocument
	doc.Version = 1
	doc.Jobs = nil
	b, err := os.ReadFile(cronJobsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return doc, err
	}
	if len(b) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return jobsDocument{Version: 1, Jobs: nil}, nil
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	if doc.Jobs == nil {
		doc.Jobs = []json.RawMessage{}
	}
	return doc, nil
}

func saveJobsDoc(doc jobsDocument) error {
	return atomicWriteJSON(cronJobsPath(), doc)
}

func loadRuntimeDoc() cronRuntimeDocument {
	var doc cronRuntimeDocument
	b, err := os.ReadFile(cronRuntimePath())
	if err != nil || len(b) == 0 {
		return cronRuntimeDocument{
			Paused: map[string]bool{},
			States: map[string]map[string]any{},
		}
	}
	if json.Unmarshal(b, &doc) != nil {
		return cronRuntimeDocument{
			Paused: map[string]bool{},
			States: map[string]map[string]any{},
		}
	}
	if doc.Paused == nil {
		doc.Paused = map[string]bool{}
	}
	if doc.States == nil {
		doc.States = map[string]map[string]any{}
	}
	return doc
}

func saveRuntimeDoc(doc cronRuntimeDocument) error {
	return atomicWriteJSON(cronRuntimePath(), doc)
}

func jobIDFromRaw(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if id, ok := m["id"].(string); ok {
		return id
	}
	return ""
}

func findJobIndex(doc *jobsDocument, jobID string) int {
	for i, raw := range doc.Jobs {
		if jobIDFromRaw(raw) == jobID {
			return i
		}
	}
	return -1
}

func newCronJobUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		b[10:16],
	)
}

func rawToMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ListCronJobs GET /api/cron/jobs — 返回任务 spec 列表（与 Python list_jobs 一致）。
func (cc *CronController) ListCronJobs(c *gin.Context) {
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(doc.Jobs))
	for _, raw := range doc.Jobs {
		m, err := rawToMap(raw)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	c.JSON(http.StatusOK, out)
}

// GetCronJob GET /api/cron/jobs/:job_id — 返回 { "spec", "state" }（CronJobView）。
func (cc *CronController) GetCronJob(c *gin.Context) {
	jobID := c.Param("job_id")
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idx := findJobIndex(&doc, jobID)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	spec, _ := rawToMap(doc.Jobs[idx])
	rt := loadRuntimeDoc()
	st := rt.States[jobID]
	if st == nil {
		st = map[string]any{
			"next_run_at": nil,
			"last_run_at": nil,
			"last_status": nil,
			"last_error":  nil,
		}
	}
	c.JSON(http.StatusOK, gin.H{"spec": spec, "state": st})
}

// CreateCronJob POST /api/cron/jobs — 服务端生成 id，忽略客户端传入的 id。
func (cc *CronController) CreateCronJob(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	jobID := newCronJobUUID()
	body["id"] = jobID
	raw, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	doc.Jobs = append(doc.Jobs, json.RawMessage(raw))
	if err := saveJobsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}

// ReplaceCronJob PUT /api/cron/jobs/:job_id — 要求 body.id 与路径一致。
func (cc *CronController) ReplaceCronJob(c *gin.Context) {
	jobID := c.Param("job_id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	if id, _ := body["id"].(string); id != jobID {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "job_id mismatch"})
		return
	}
	raw, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idx := findJobIndex(&doc, jobID)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	doc.Jobs[idx] = json.RawMessage(raw)
	if err := saveJobsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}

// DeleteCronJob DELETE /api/cron/jobs/:job_id
func (cc *CronController) DeleteCronJob(c *gin.Context) {
	jobID := c.Param("job_id")
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idx := findJobIndex(&doc, jobID)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	doc.Jobs = append(doc.Jobs[:idx], doc.Jobs[idx+1:]...)
	if err := saveJobsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	rt := loadRuntimeDoc()
	delete(rt.Paused, jobID)
	delete(rt.States, jobID)
	_ = saveRuntimeDoc(rt)
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// PauseCronJob POST /api/cron/jobs/:job_id/pause — 将暂停标记写入 cron_runtime.json。
func (cc *CronController) PauseCronJob(c *gin.Context) {
	jobID := c.Param("job_id")
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if findJobIndex(&doc, jobID) < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	rt := loadRuntimeDoc()
	rt.Paused[jobID] = true
	if err := saveRuntimeDoc(rt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"paused": true})
}

// ResumeCronJob POST /api/cron/jobs/:job_id/resume
func (cc *CronController) ResumeCronJob(c *gin.Context) {
	jobID := c.Param("job_id")
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if findJobIndex(&doc, jobID) < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	rt := loadRuntimeDoc()
	delete(rt.Paused, jobID)
	if err := saveRuntimeDoc(rt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"resumed": true})
}

// RunCronJob POST /api/cron/jobs/:job_id/run — 异步执行：agent 任务调用与 /api/agent/process 相同模型；text 任务仅标记成功。
func (cc *CronController) RunCronJob(c *gin.Context) {
	jobID := c.Param("job_id")
	cronMu.Lock()
	doc, err := loadJobsDoc()
	if err != nil {
		cronMu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idx := findJobIndex(&doc, jobID)
	if idx < 0 {
		cronMu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	raw := doc.Jobs[idx]
	cronMu.Unlock()

	taskType, _, userText, sessionKey, userID, dispatchChannel, timeoutSec := parseCronJobPayload(raw)

	cronMu.Lock()
	rt := loadRuntimeDoc()
	if rt.States[jobID] == nil {
		rt.States[jobID] = map[string]any{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rt.States[jobID]["last_run_at"] = now
	rt.States[jobID]["last_status"] = "running"
	rt.States[jobID]["last_error"] = nil
	if err := saveRuntimeDoc(rt); err != nil {
		cronMu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	cronMu.Unlock()

	go runCronJobAsync(jobID, taskType, userText, sessionKey, userID, dispatchChannel, timeoutSec)

	c.JSON(http.StatusOK, gin.H{"started": true})
}

func parseCronJobPayload(raw json.RawMessage) (taskType, cronText, userText, sessionKey, userID, dispatchChannel string, timeoutSec int) {
	var job map[string]any
	if json.Unmarshal(raw, &job) != nil {
		return "agent", "", "", normSessionKey(""), "", "", 120
	}
	if t, ok := job["task_type"].(string); ok && t != "" {
		taskType = t
	} else {
		taskType = "agent"
	}
	if t, ok := job["text"].(string); ok {
		cronText = t
	}
	var reqInput any
	if req, ok := job["request"].(map[string]any); ok {
		reqInput = req["input"]
		if sid, ok := req["session_id"].(string); ok && strings.TrimSpace(sid) != "" {
			sessionKey = sid
		}
	}
	if disp, ok := job["dispatch"].(map[string]any); ok {
		if ch, ok := disp["channel"].(string); ok {
			dispatchChannel = strings.TrimSpace(ch)
		}
		if tgt, ok := disp["target"].(map[string]any); ok {
			if strings.TrimSpace(sessionKey) == "" {
				sessionKey, _ = tgt["session_id"].(string)
			}
			if u, ok := tgt["user_id"].(string); ok {
				userID = strings.TrimSpace(u)
			}
		}
	}
	switch taskType {
	case "text":
		userText = strings.TrimSpace(cronText)
	default:
		userText = strings.TrimSpace(extractLastUserText(reqInput))
	}
	timeoutSec = 120
	if rt, ok := job["runtime"].(map[string]any); ok {
		if v, ok := rt["timeout_seconds"]; ok {
			if n, ok := toInt64(v); ok && n > 0 {
				timeoutSec = int(n)
			}
		}
	}
	sessionKey = normSessionKey(sessionKey)
	return taskType, cronText, userText, sessionKey, userID, dispatchChannel, timeoutSec
}

func runCronJobAsync(jobID, taskType, userText, sessionKey, userID, dispatchChannel string, timeoutSec int) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var runErr error
	switch taskType {
	case "text":
		channels.DispatchText(dispatchChannel, sessionKey, userID, userText, false)
	default:
		if strings.TrimSpace(userText) == "" {
			runErr = fmt.Errorf("empty cron agent input")
		} else {
			var reply string
			reply, runErr = RunCronAgentTask(ctx, sessionKey, userText)
			if runErr == nil {
				channels.DispatchText(dispatchChannel, sessionKey, userID, reply, false)
			}
		}
	}

	cronMu.Lock()
	defer cronMu.Unlock()
	rt := loadRuntimeDoc()
	if rt.States[jobID] == nil {
		rt.States[jobID] = map[string]any{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rt.States[jobID]["last_run_at"] = now
	if runErr != nil {
		rt.States[jobID]["last_status"] = "error"
		rt.States[jobID]["last_error"] = runErr.Error()
	} else {
		rt.States[jobID]["last_status"] = "success"
		rt.States[jobID]["last_error"] = nil
	}
	_ = saveRuntimeDoc(rt)
}

// GetCronJobState GET /api/cron/jobs/:job_id/state — 仅返回 CronJobState 对象。
func (cc *CronController) GetCronJobState(c *gin.Context) {
	jobID := c.Param("job_id")
	cronMu.Lock()
	defer cronMu.Unlock()
	doc, err := loadJobsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if findJobIndex(&doc, jobID) < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "job not found"})
		return
	}
	rt := loadRuntimeDoc()
	st := rt.States[jobID]
	if st == nil {
		st = map[string]any{
			"next_run_at": nil,
			"last_run_at": nil,
			"last_status": nil,
			"last_error":  nil,
		}
	}
	c.JSON(http.StatusOK, st)
}
