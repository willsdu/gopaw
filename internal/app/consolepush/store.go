// Package consolepush 提供控制台轮询推送的内存队列（与 copaw console_push_store 一致）。
package consolepush

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

const (
	// DefaultMaxAgeSeconds 无 session_id 拉取时仅返回该时间窗口内的消息。
	DefaultMaxAgeSeconds = 60
	maxMessages          = 500
)

// Message 返回给前端的 JSON 形态（不含 ts/session_id）。
type Message struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Sticky bool   `json:"sticky"`
}

type entry struct {
	ID        string
	Text      string
	Sticky    bool
	TS        float64
	SessionID string
}

var (
	mu     sync.Mutex
	list   []entry
	nowFn  = time.Now
)

// NowFunc 可注入用于测试。
func NowFunc(f func() time.Time) {
	mu.Lock()
	defer mu.Unlock()
	if f == nil {
		nowFn = time.Now
		return
	}
	nowFn = f
}

// Append 追加一条推送；session_id 或 text 为空则忽略。
func Append(sessionID, text string, sticky bool) {
	sessionID = trimSpace(sessionID)
	text = trimSpace(text)
	if sessionID == "" || text == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	list = append(list, entry{
		ID:        randomHexID(),
		Text:      text,
		Sticky:    sticky,
		TS:        float64(nowFn().UnixNano()) / 1e9,
		SessionID: sessionID,
	})
	if len(list) > maxMessages {
		sort.Slice(list, func(i, j int) bool { return list[i].TS < list[j].TS })
		list = list[len(list)-maxMessages:]
	}
}

// TakeForSession 取出并移除该会话的全部消息。
func TakeForSession(sessionID string) []Message {
	if sessionID == "" {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	var taken []entry
	rest := list[:0]
	for _, m := range list {
		if m.SessionID == sessionID {
			taken = append(taken, m)
		} else {
			rest = append(rest, m)
		}
	}
	list = rest
	return strip(taken)
}

// GetRecent 返回最近窗口内消息且不消费，并丢弃过期项。
func GetRecent(maxAgeSeconds int) []Message {
	if maxAgeSeconds <= 0 {
		maxAgeSeconds = DefaultMaxAgeSeconds
	}
	now := float64(nowFn().UnixNano()) / 1e9
	cutoff := now - float64(maxAgeSeconds)
	mu.Lock()
	defer mu.Unlock()
	var kept []entry
	var out []entry
	for _, m := range list {
		if m.TS >= cutoff {
			kept = append(kept, m)
			out = append(out, m)
		}
	}
	list = kept
	return strip(out)
}

func strip(msgs []entry) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, Message{ID: m.ID, Text: m.Text, Sticky: m.Sticky})
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func randomHexID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
