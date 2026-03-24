package routers

// Twilio ConversationRelay：POST /voice/incoming 返回 TwiML；GET /voice/ws 处理转写与 LLM 回复帧。
// 配置见 voice_config.go；公网 WSS 基址用 channels.voice 或环境变量 GOPAW_VOICE_PUBLIC_WSS_BASE。

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const voiceLLMErrorMsg = "I'm having trouble right now. Please try again."

type VoiceController struct{}

var voiceUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type twilioWSOut struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	Last  bool   `json:"last"`
}

func (vc *VoiceController) VoiceIncoming(c *gin.Context) {
	vs := loadVoiceSettings()
	if strings.TrimSpace(vs.TwilioAuthToken) != "" && !validateTwilioPOST(c, vs.TwilioAuthToken) {
		twilioForbidden(c)
		return
	}
	if !voiceChannelReady(vs) {
		twiml, err := buildErrorTwiml("Voice channel is disabled or public WebSocket URL is not configured.")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/xml", []byte(twiml))
		return
	}
	base := strings.TrimSuffix(vs.PublicWSSBase, "/")
	tok := createVoiceWSToken()
	wsURL := base + "/voice/ws?token=" + url.QueryEscape(tok)
	twiml, err := buildConversationRelayTwiml(
		wsURL,
		vs.WelcomeGreeting,
		vs.TTSSProvider,
		vs.TTSVoice,
		vs.STTProvider,
		vs.Language,
		true,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/xml", []byte(twiml))
}

func (vc *VoiceController) VoiceWS(c *gin.Context) {
	vs := loadVoiceSettings()
	if !voiceChannelReady(vs) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "voice not configured"})
		return
	}
	tok := strings.TrimSpace(c.Query("token"))
	if tok == "" || !validateConsumeVoiceWSToken(tok) {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid token"})
		return
	}
	conn, err := voiceUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	callSid := ""
	sendText := func(token string, last bool) error {
		return conn.WriteJSON(twilioWSOut{Type: "text", Token: token, Last: last})
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
	})

	for {
		_, payload, rerr := conn.ReadMessage()
		if rerr != nil {
			break
		}
		var msg map[string]any
		if json.Unmarshal(payload, &msg) != nil {
			continue
		}
		typ, _ := msg["type"].(string)
		switch typ {
		case "setup":
			callSid, _ = msg["callSid"].(string)
			if strings.TrimSpace(callSid) == "" {
				_ = conn.WriteJSON(map[string]string{"type": "end"})
				return
			}
		case "prompt":
			if strings.TrimSpace(callSid) == "" {
				continue
			}
			text, _ := msg["voicePrompt"].(string)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			voiceRunLLM(ctx, callSid, text, sendText)
			cancel()
		case "interrupt":
			u, _ := msg["utteranceUntilInterrupt"].(string)
			log.Printf("voice interrupt call_sid=%s utterance=%s", callSid, trim100(u))
		case "dtmf":
			d, _ := msg["digit"].(string)
			log.Printf("voice dtmf call_sid=%s digit=%s", callSid, d)
		default:
		}
	}
}

func trim100(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 100 {
		return s
	}
	return s[:100]
}

func voiceRunLLM(ctx context.Context, callSid, userText string, sendText func(string, bool) error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return
	}

	st, err := loadProvidersState()
	if err != nil {
		_ = sendText(voiceLLMErrorMsg, false)
		_ = sendText("", true)
		return
	}
	p, ok := st.Providers[st.Active.ProviderID]
	if !ok || strings.TrimSpace(st.Active.Model) == "" || strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.BaseURL) == "" {
		_ = sendText(voiceLLMErrorMsg, false)
		_ = sendText("", true)
		return
	}

	sess := normSessionKey("voice:" + callSid)
	apiMsgs := buildAPIMessages(sess, userText)
	text, err := executeNonStreamChat(ctx, st, p, apiMsgs)
	if err != nil {
		log.Printf("voice llm error call_sid=%s: %v", callSid, err)
		_ = sendText(voiceLLMErrorMsg, false)
		_ = sendText("", true)
		return
	}
	globalSessions.appendUserThenAssistant(sess, userText, text)
	persistProcessTurnToChats(sess, userText, text)
	if strings.TrimSpace(text) != "" {
		_ = sendText(text, false)
	}
	_ = sendText("", true)
}

func (vc *VoiceController) VoiceStatusCallback(c *gin.Context) {
	vs := loadVoiceSettings()
	if strings.TrimSpace(vs.TwilioAuthToken) != "" && !validateTwilioPOST(c, vs.TwilioAuthToken) {
		twilioForbidden(c)
		return
	}
	_ = c.Request.ParseForm()
	callSid := c.PostForm("CallSid")
	status := c.PostForm("CallStatus")
	log.Printf("voice status callback call_sid=%s status=%s", callSid, status)
	c.Status(http.StatusNoContent)
}
