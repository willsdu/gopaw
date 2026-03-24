package routers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/willisdu/gopaw/internal/config"
)

// 本文件实现 /api/chats/*，与 copaw/app/runner/api.py 对齐。
// 会话元数据写入 chats.json；消息正文由 chat_histories.json 持久化，
// 在 /api/agent/process 与 Cron agent 任务成功后会按 session_id 关联写入。

const chatsFileName = "chats.json"

var chatsMu sync.Mutex

type chatsDocument struct {
	Version int               `json:"version"`
	Chats   []json.RawMessage `json:"chats"`
}

type ChatsController struct{}

func chatsPath() string {
	return filepath.Join(config.WorkingDir(), chatsFileName)
}

func loadChatsDoc() (chatsDocument, error) {
	var doc chatsDocument
	doc.Version = 1
	doc.Chats = nil
	b, err := os.ReadFile(chatsPath())
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
		return chatsDocument{Version: 1, Chats: nil}, nil
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	if doc.Chats == nil {
		doc.Chats = []json.RawMessage{}
	}
	return doc, nil
}

func saveChatsDoc(doc chatsDocument) error {
	return atomicWriteJSON(chatsPath(), doc)
}

func chatIDFromRaw(raw json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if id, ok := m["id"].(string); ok {
		return id
	}
	return ""
}

func findChatIndex(doc *chatsDocument, chatID string) int {
	for i, raw := range doc.Chats {
		if chatIDFromRaw(raw) == chatID {
			return i
		}
	}
	return -1
}

// ListChats GET /api/chats — 支持 query user_id、channel 过滤。
func (cc *ChatsController) ListChats(c *gin.Context) {
	userFilter := c.Query("user_id")
	channelFilter := c.Query("channel")
	chatsMu.Lock()
	defer chatsMu.Unlock()
	doc, err := loadChatsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(doc.Chats))
	for _, raw := range doc.Chats {
		m, err := rawToMap(raw)
		if err != nil {
			continue
		}
		if userFilter != "" {
			if u, _ := m["user_id"].(string); u != userFilter {
				continue
			}
		}
		if channelFilter != "" {
			if ch, _ := m["channel"].(string); ch != channelFilter {
				continue
			}
		}
		out = append(out, m)
	}
	c.JSON(http.StatusOK, out)
}

// CreateChat POST /api/chats — 服务端生成 UUID，写入时间戳。
func (cc *ChatsController) CreateChat(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	sid, _ := body["session_id"].(string)
	uid, _ := body["user_id"].(string)
	if sid == "" || uid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "session_id and user_id are required"})
		return
	}
	chatID := newCronJobUUID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body["id"] = chatID
	if _, ok := body["name"]; !ok {
		body["name"] = "New Chat"
	}
	if _, ok := body["channel"]; !ok {
		body["channel"] = "console"
	}
	body["created_at"] = now
	body["updated_at"] = now
	if _, ok := body["meta"]; !ok {
		body["meta"] = map[string]any{}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	chatsMu.Lock()
	defer chatsMu.Unlock()
	doc, err := loadChatsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	doc.Chats = append(doc.Chats, json.RawMessage(raw))
	if err := saveChatsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}

// BatchDeleteChats POST /api/chats/batch-delete — body 为 chat id 数组。
func (cc *ChatsController) BatchDeleteChats(c *gin.Context) {
	var ids []string
	if err := c.ShouldBindJSON(&ids); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	chatsMu.Lock()
	defer chatsMu.Unlock()
	doc, err := loadChatsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idSet := map[string]struct{}{}
	for _, id := range ids {
		idSet[id] = struct{}{}
	}
	var next []json.RawMessage
	deletedCount := 0
	for _, raw := range doc.Chats {
		cid := chatIDFromRaw(raw)
		if _, drop := idSet[cid]; drop {
			deletedCount++
			continue
		}
		next = append(next, raw)
	}
	doc.Chats = next
	if err := saveChatsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"deleted_count": deletedCount,
	})
}

// GetChat GET /api/chats/:chat_id — 返回 ChatHistory；messages 来自 chat_histories.json（由 /api/agent/process 按 session 同步）。
func (cc *ChatsController) GetChat(c *gin.Context) {
	chatID := c.Param("chat_id")
	chatsMu.Lock()
	doc, err := loadChatsDoc()
	chatsMu.Unlock()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if findChatIndex(&doc, chatID) < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Chat not found: " + chatID})
		return
	}
	msgs, err := listChatHistoryMessages(chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{"role": m.Role, "content": m.Content})
	}
	c.JSON(http.StatusOK, gin.H{"messages": out})
}

// UpdateChat PUT /api/chats/:chat_id
func (cc *ChatsController) UpdateChat(c *gin.Context) {
	chatID := c.Param("chat_id")
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "invalid json"})
		return
	}
	if id, _ := body["id"].(string); id != chatID {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "chat_id mismatch"})
		return
	}
	chatsMu.Lock()
	defer chatsMu.Unlock()
	doc, err := loadChatsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idx := findChatIndex(&doc, chatID)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Chat not found: " + chatID})
		return
	}
	body["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	doc.Chats[idx] = json.RawMessage(raw)
	if err := saveChatsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, body)
}

// DeleteChat DELETE /api/chats/:chat_id
func (cc *ChatsController) DeleteChat(c *gin.Context) {
	chatID := c.Param("chat_id")
	chatsMu.Lock()
	defer chatsMu.Unlock()
	doc, err := loadChatsDoc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	idx := findChatIndex(&doc, chatID)
	if idx < 0 {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Chat not found: " + chatID})
		return
	}
	doc.Chats = append(doc.Chats[:idx], doc.Chats[idx+1:]...)
	if err := saveChatsDoc(doc); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"chat_id": chatID,
	})
}
