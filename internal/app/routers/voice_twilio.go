package routers

// Twilio 请求签名校验（与 copaw RequestValidator 一致）及 ConversationRelay TwiML。

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

func twilioPublicURL(c *gin.Context) string {
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	path := c.Request.URL.Path
	u := proto + "://" + host + path
	if q := c.Request.URL.RawQuery; q != "" {
		u += "?" + q
	}
	return u
}

func twilioSignatureOK(authToken, fullURL string, form url.Values, signatureHeader string) bool {
	if authToken == "" || signatureHeader == "" {
		return false
	}
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		vs := form[k]
		if len(vs) == 0 {
			continue
		}
		v := vs[len(vs)-1]
		sb.WriteString(k)
		sb.WriteString(v)
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(sb.String()))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	sig := strings.TrimSpace(signatureHeader)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1
}

func validateTwilioPOST(c *gin.Context, authToken string) bool {
	if strings.TrimSpace(authToken) == "" {
		return true
	}
	sig := c.GetHeader("X-Twilio-Signature")
	if sig == "" {
		return false
	}
	if err := c.Request.ParseForm(); err != nil {
		return false
	}
	return twilioSignatureOK(authToken, twilioPublicURL(c), c.Request.PostForm, sig)
}

func twilioForbidden(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"detail": "Invalid Twilio signature"})
}

type twimlConvRelay struct {
	URL                    string `xml:"url,attr"`
	WelcomeGreeting        string `xml:"welcomeGreeting,attr"`
	TTSProvider            string `xml:"ttsProvider,attr"`
	Voice                  string `xml:"voice,attr"`
	TranscriptionProvider  string `xml:"transcriptionProvider,attr"`
	Language               string `xml:"language,attr"`
	Interruptible          string `xml:"interruptible,attr"`
}

type twimlConnect struct {
	Relay twimlConvRelay `xml:"ConversationRelay"`
}

type twimlResponse struct {
	XMLName xml.Name     `xml:"Response"`
	Connect twimlConnect `xml:"Connect"`
}

func newTwimlResponse(connect twimlConnect) twimlResponse {
	return twimlResponse{
		XMLName: xml.Name{Local: "Response"},
		Connect: connect,
	}
}

type twimlSayResponse struct {
	XMLName xml.Name `xml:"Response"`
	Say     string   `xml:"Say"`
}

func newTwimlSay(msg string) twimlSayResponse {
	return twimlSayResponse{XMLName: xml.Name{Local: "Response"}, Say: msg}
}

func buildConversationRelayTwiml(wsURL string, welcome, ttsProvider, ttsVoice, sttProvider, lang string, interruptible bool) (string, error) {
	inter := "false"
	if interruptible {
		inter = "true"
	}
	r := newTwimlResponse(twimlConnect{
		Relay: twimlConvRelay{
			URL:                   wsURL,
			WelcomeGreeting:       welcome,
			TTSProvider:           ttsProvider,
			Voice:                 ttsVoice,
			TranscriptionProvider: sttProvider,
			Language:              lang,
			Interruptible:         inter,
		},
	})
	b, err := xml.MarshalIndent(r, "", "")
	if err != nil {
		return "", err
	}
	return xml.Header + string(b), nil
}

func buildErrorTwiml(message string) (string, error) {
	r := newTwimlSay(message)
	b, err := xml.MarshalIndent(r, "", "")
	if err != nil {
		return "", err
	}
	return xml.Header + string(b), nil
}
