package routers

// Twilio incoming 生成一次性 WebSocket token，/voice/ws 校验后消费（与 copaw VoiceChannel 一致）。

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
)

const maxPendingVoiceWSTokens = 100

var voiceWSTokens = struct {
	mu    sync.Mutex
	order []string
	set   map[string]struct{}
}{
	set: make(map[string]struct{}),
}

func createVoiceWSToken() string {
	voiceWSTokens.mu.Lock()
	defer voiceWSTokens.mu.Unlock()
	for len(voiceWSTokens.order) >= maxPendingVoiceWSTokens {
		old := voiceWSTokens.order[0]
		voiceWSTokens.order = voiceWSTokens.order[1:]
		delete(voiceWSTokens.set, old)
	}
	var b [24]byte
	_, _ = rand.Read(b[:])
	tok := base64.RawURLEncoding.EncodeToString(b[:])
	voiceWSTokens.set[tok] = struct{}{}
	voiceWSTokens.order = append(voiceWSTokens.order, tok)
	return tok
}

func validateConsumeVoiceWSToken(tok string) bool {
	if tok == "" {
		return false
	}
	voiceWSTokens.mu.Lock()
	defer voiceWSTokens.mu.Unlock()
	if _, ok := voiceWSTokens.set[tok]; !ok {
		return false
	}
	delete(voiceWSTokens.set, tok)
	for i, x := range voiceWSTokens.order {
		if x == tok {
			voiceWSTokens.order = append(voiceWSTokens.order[:i], voiceWSTokens.order[i+1:]...)
			break
		}
	}
	return true
}
