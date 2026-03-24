package routers

// 将控制台对话轮次持久化到 chat_histories.json，供 GET /api/chats/:id 返回 messages（与 copaw 侧「会话可查」行为接近）。
// 通过 session_id 关联 chats.json 中的聊天项；同一 session 可对应多个 chat UUID（各写一份副本）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/willisdu/gopaw/internal/config"
)

const chatHistoriesFile = "chat_histories.json"

var chatHistMu sync.Mutex

type chatHistoriesDoc struct {
	Version int                     `json:"version"`
	Chats   map[string][]historyMsg `json:"chats"`
}

type historyMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func chatHistoriesPath() string {
	return filepath.Join(config.WorkingDir(), chatHistoriesFile)
}

func loadChatHistoriesDoc() (chatHistoriesDoc, error) {
	var doc chatHistoriesDoc
	doc.Version = 1
	doc.Chats = map[string][]historyMsg{}
	b, err := os.ReadFile(chatHistoriesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return doc, err
	}
	if len(b) == 0 {
		return doc, nil
	}
	if json.Unmarshal(b, &doc) != nil {
		return chatHistoriesDoc{Version: 1, Chats: map[string][]historyMsg{}}, nil
	}
	if doc.Chats == nil {
		doc.Chats = map[string][]historyMsg{}
	}
	return doc, nil
}

func saveChatHistoriesDoc(doc chatHistoriesDoc) error {
	return atomicWriteJSON(chatHistoriesPath(), doc)
}

func runtimeTextContent(text string) []any {
	return []any{map[string]any{"type": "text", "text": text}}
}

func appendChatHistoryPair(chatID, userText, assistantText string) error {
	if chatID == "" {
		return nil
	}
	chatHistMu.Lock()
	defer chatHistMu.Unlock()
	doc, err := loadChatHistoriesDoc()
	if err != nil {
		return err
	}
	list := doc.Chats[chatID]
	list = append(list,
		historyMsg{Role: "user", Content: runtimeTextContent(userText)},
		historyMsg{Role: "assistant", Content: runtimeTextContent(assistantText)},
	)
	doc.Chats[chatID] = list
	return saveChatHistoriesDoc(doc)
}

func listChatHistoryMessages(chatID string) ([]historyMsg, error) {
	chatHistMu.Lock()
	defer chatHistMu.Unlock()
	doc, err := loadChatHistoriesDoc()
	if err != nil {
		return nil, err
	}
	if doc.Chats == nil {
		return []historyMsg{}, nil
	}
	out := doc.Chats[chatID]
	if out == nil {
		return []historyMsg{}, nil
	}
	cp := make([]historyMsg, len(out))
	copy(cp, out)
	return cp, nil
}

// persistProcessTurnToChats 在 /api/agent/process 成功后调用：按 session 匹配 chats.json 中的条目并追加消息。
// key 须为已规范化的 session 键（与 globalSessions 一致，见 normSessionKey）。
func persistProcessTurnToChats(key, userText, assistantText string) {
	chatsMu.Lock()
	doc, err := loadChatsDoc()
	var ids []string
	if err == nil {
		for _, raw := range doc.Chats {
			m, e := rawToMap(raw)
			if e != nil {
				continue
			}
			sid, _ := m["session_id"].(string)
			if normSessionKey(sid) != key {
				continue
			}
			if id, ok := m["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	chatsMu.Unlock()
	for _, id := range ids {
		_ = appendChatHistoryPair(id, userText, assistantText)
	}
}
