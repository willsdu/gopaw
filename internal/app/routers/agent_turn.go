package routers

// 非流式对话与定时任务复用的上游调用逻辑（与 POST /api/agent/process 非流式分支一致）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var errUnsupportedChatProvider = errors.New("provider not supported for chat")

func buildAPIMessages(sessionKey, userText string) []openAIChatMessage {
	hist := globalSessions.getCopy(sessionKey)
	apiMsgs := make([]openAIChatMessage, 0, len(hist)+1)
	for _, m := range hist {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "system" {
			continue
		}
		apiMsgs = append(apiMsgs, openAIChatMessage{Role: m.Role, Content: m.Content})
	}
	apiMsgs = append(apiMsgs, openAIChatMessage{Role: "user", Content: userText})
	return apiMsgs
}

func providerExtraMap(p ProviderInfo) map[string]any {
	extra := map[string]any{}
	if p.GenerateKw != nil {
		for k, v := range p.GenerateKw {
			extra[k] = v
		}
	}
	return extra
}

// executeNonStreamChat 调用激活模型完成一轮补全（不写 session、不落盘），并写入 token_usage.json。
func executeNonStreamChat(ctx context.Context, st providersState, p ProviderInfo, apiMsgs []openAIChatMessage) (string, error) {
	extra := providerExtraMap(p)
	if providerIsAnthropic(p) {
		sys, ams := openAIMessagesToAnthropic(apiMsgs)
		if len(ams) == 0 {
			return "", fmt.Errorf("no user/assistant messages for anthropic")
		}
		aurl := anthropicMessagesURL(p.BaseURL)
		maxTok := anthropicMaxTokens(extra, p)
		text, u, err := runAnthropicMessagesNonStream(ctx, p.APIKey, aurl, st.Active.Model, sys, ams, maxTok)
		if err != nil {
			return "", err
		}
		RecordTokenUsage(st.Active.ProviderID, st.Active.Model, u.PromptTokens, u.CompletionTokens)
		return text, nil
	}
	if !providerUsesOpenAICompat(p) {
		return "", errUnsupportedChatProvider
	}
	url := chatCompletionsURL(p.BaseURL)
	text, u, err := runOpenAIChatNonStream(ctx, p.APIKey, url, st.Active.Model, apiMsgs, extra)
	if err != nil {
		return "", err
	}
	RecordTokenUsage(st.Active.ProviderID, st.Active.Model, u.PromptTokens, u.CompletionTokens)
	return text, nil
}

// RunCronAgentTask 供 Cron「agent」任务调用：走与非流式 process 相同模型与会话，并写入内存 session 与 chat_histories。
// 成功时返回助手回复正文，供 dispatch 到 console 等通道。
func RunCronAgentTask(ctx context.Context, sessionKey, userText string) (reply string, err error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", fmt.Errorf("empty user text")
	}
	st, e := loadProvidersState()
	if e != nil {
		return "", e
	}
	p, ok := st.Providers[st.Active.ProviderID]
	if !ok {
		return "", fmt.Errorf("active provider not found")
	}
	if strings.TrimSpace(st.Active.Model) == "" {
		return "", fmt.Errorf("no active model")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return "", fmt.Errorf("missing API key")
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return "", fmt.Errorf("missing base_url")
	}
	key := normSessionKey(sessionKey)
	apiMsgs := buildAPIMessages(key, userText)
	text, e := executeNonStreamChat(ctx, st, p, apiMsgs)
	if e != nil {
		return "", e
	}
	globalSessions.appendUserThenAssistant(key, userText, text)
	persistProcessTurnToChats(key, userText, text)
	return text, nil
}
