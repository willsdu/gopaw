package routers

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// 内存会话历史：控制台每次只传最后一轮 user input，需服务端按 session_id 拼接多轮。
// 与 copaw AgentApp 行为对齐的简化版（进程重启后丢失）。

const (
	maxMsgsPerSess  = 80
	userMsgTruncate = 32000
)

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type sessionStore struct {
	mu   sync.Mutex
	data map[string][]chatMsg
}

var globalSessions = &sessionStore{data: make(map[string][]chatMsg)}

func randomID(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}

func normSessionKey(id string) string {
	if id == "" {
		return "_default"
	}
	return id
}

func (s *sessionStore) getCopy(key string) []chatMsg {
	s.mu.Lock()
	defer s.mu.Unlock()
	ms := s.data[key]
	if len(ms) == 0 {
		return nil
	}
	out := make([]chatMsg, len(ms))
	copy(out, ms)
	return out
}

func (s *sessionStore) appendUserThenAssistant(key string, userText, assistantText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ms := s.data[key]
	u := userText
	if len(u) > userMsgTruncate {
		u = u[:userMsgTruncate]
	}
	ms = append(ms, chatMsg{Role: "user", Content: u})
	if assistantText != "" {
		a := assistantText
		if len(a) > userMsgTruncate {
			a = a[:userMsgTruncate]
		}
		ms = append(ms, chatMsg{Role: "assistant", Content: a})
	}
	if len(ms) > maxMsgsPerSess {
		ms = ms[len(ms)-maxMsgsPerSess:]
	}
	s.data[key] = ms
}
