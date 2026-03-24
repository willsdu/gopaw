package routers

// 从 app_config.json 的 channels 与可选环境变量解析 Twilio ConversationRelay 所需配置。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopaw/internal/config"
)

const envVoicePublicWSS = "GOPAW_VOICE_PUBLIC_WSS_BASE"

type voiceSettings struct {
	Enabled          bool
	TwilioAuthToken  string
	TwilioAccountSID string
	WelcomeGreeting  string
	TTSSProvider     string
	TTSVoice         string
	STTProvider      string
	Language         string
	// PublicWSSBase 为 wss://host（可含路径前缀），末尾不要斜杠；用于拼 TwiML 里 WebSocket URL。
	PublicWSSBase string
}

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%.0f", t)
		}
		return fmt.Sprintf("%g", t)
	case json.Number:
		return strings.TrimSpace(string(t))
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func truthyAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return t != 0
	default:
		return false
	}
}

func ensureWSSBase(raw string) string {
	s := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "https://") {
		return "wss://" + strings.TrimPrefix(s, "https://")
	}
	if strings.HasPrefix(s, "http://") {
		return "wss://" + strings.TrimPrefix(s, "http://")
	}
	if strings.HasPrefix(s, "wss://") || strings.HasPrefix(s, "ws://") {
		return s
	}
	return "wss://" + s
}

func findVoiceChannelMap() map[string]any {
	b, err := os.ReadFile(appConfigPath())
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	chRaw, ok := root["channels"]
	if !ok || chRaw == nil {
		return nil
	}
	switch v := chRaw.(type) {
	case map[string]any:
		if voice, ok := v["voice"].(map[string]any); ok {
			return voice
		}
	case []any:
		for _, it := range v {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if s := strFromMap(m, "channel"); strings.EqualFold(s, "voice") {
				return m
			}
			if s := strFromMap(m, "type"); strings.EqualFold(s, "voice") {
				return m
			}
		}
	}
	st, err := loadAppConfigState()
	if err == nil {
		if vm, ok := parseChannelsBlob(st.Channels)["voice"].(map[string]any); ok {
			return vm
		}
	}
	// 与 copaw 共用工作区时，channels.voice 可能在 config.json 根下。
	if m := voiceFromMainConfigJSON(); m != nil {
		return m
	}
	return nil
}

func voiceFromMainConfigJSON() map[string]any {
	p := filepath.Join(config.WorkingDir(), "config.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return nil
	}
	chRaw, ok := root["channels"]
	if !ok {
		return nil
	}
	ch, ok := chRaw.(map[string]any)
	if !ok {
		return nil
	}
	voice, ok := ch["voice"].(map[string]any)
	if !ok {
		return nil
	}
	return voice
}

func loadVoiceSettings() voiceSettings {
	def := voiceSettings{
		WelcomeGreeting: "Hi! This is GoPaw. How can I help you?",
		TTSSProvider:    "google",
		TTSVoice:        "en-US-Journey-D",
		STTProvider:     "deepgram",
		Language:        "en-US",
	}
	vm := findVoiceChannelMap()
	if vm != nil {
		def.Enabled = truthyAny(vm["enabled"])
		def.TwilioAuthToken = strFromMap(vm, "twilio_auth_token")
		def.TwilioAccountSID = strFromMap(vm, "twilio_account_sid")
		if s := strFromMap(vm, "welcome_greeting"); s != "" {
			def.WelcomeGreeting = s
		}
		if s := strFromMap(vm, "tts_provider"); s != "" {
			def.TTSSProvider = s
		}
		if s := strFromMap(vm, "tts_voice"); s != "" {
			def.TTSVoice = s
		}
		if s := strFromMap(vm, "stt_provider"); s != "" {
			def.STTProvider = s
		}
		if s := strFromMap(vm, "language"); s != "" {
			def.Language = s
		}
		for _, k := range []string{"public_wss_url", "tunnel_wss_url", "wss_url", "public_url"} {
			if s := strFromMap(vm, k); s != "" {
				def.PublicWSSBase = ensureWSSBase(s)
				break
			}
		}
	}
	if e := strings.TrimSpace(os.Getenv(envVoicePublicWSS)); e != "" {
		def.PublicWSSBase = ensureWSSBase(e)
	}
	return def
}

func voiceChannelReady(v voiceSettings) bool {
	if !v.Enabled {
		return false
	}
	return v.PublicWSSBase != ""
}
