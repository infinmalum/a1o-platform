package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
	openaiadapter "github.com/infinmalum/one-gateway/relay/adaptor/openai"
)

const (
	// defaultMaxTokens bounds one assistant leg when the session sets none.
	defaultMaxTokens = int64(4096)
	// compactSummarizerTokens bounds the summarization leg's output.
	compactSummarizerTokens = int64(1536)
	// compactTranscriptChars caps one message's render inside the transcript.
	compactTranscriptChars = 4000
)

// openAIMessage mirrors the chat completions message fields the runtime uses.
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// contentPart is one multimodal content part; text and image_url are the two
// kinds the runtime produces.
type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// historyItem couples one stored message with its wire form and estimate.
type historyItem struct {
	row    *model.ChatMessage
	msg    openAIMessage
	tokens int
}

// compactPrompt instructs the summarizer leg.
const compactPrompt = "你是对话摘要助手。请把下面的对话历史压缩成一份简洁的摘要，保留关键事实、做出的决定、重要代码与命令要点、以及未完成的任务。直接输出摘要正文，不要任何开场白。"

// estimateText counts tokens with the shared tiktoken helper; uninitialized
// encoders degrade to the approximation path.
func estimateText(text string, modelName string) int {
	return openaiadapter.CountTokenText(text, modelName)
}

// loadHistoryFiles collects the attachment payloads referenced anywhere in
// the working history. Missing or foreign files resolve to placeholders.
func loadHistoryFiles(session *model.ChatSession, history []*model.ChatMessage) (map[int]*model.ChatFile, error) {
	ids := map[int]bool{}
	for _, row := range history {
		if row.Parts == nil || *row.Parts == "" {
			continue
		}
		var parts []model.ChatMessagePart
		if err := json.Unmarshal([]byte(*row.Parts), &parts); err != nil {
			continue
		}
		for _, part := range parts {
			if part.FileID != 0 {
				ids[part.FileID] = true
			}
		}
	}
	files := make(map[int]*model.ChatFile, len(ids))
	for id := range ids {
		file, err := model.GetChatFileById(id, session.UserId)
		if err != nil {
			continue
		}
		files[id] = file
	}
	return files, nil
}

// resolveParts expands a user message's stored attachments into content
// parts, inlining text payloads and converting images to data URLs.
func resolveParts(msg *model.ChatMessage, files map[int]*model.ChatFile) (any, error) {
	if msg.Parts == nil || *msg.Parts == "" {
		return msg.Content, nil
	}
	var stored []model.ChatMessagePart
	if err := json.Unmarshal([]byte(*msg.Parts), &stored); err != nil {
		return msg.Content, nil
	}
	parts := make([]contentPart, 0, len(stored)+1)
	if msg.Content != "" {
		parts = append(parts, contentPart{Type: "text", Text: msg.Content})
	}
	for _, part := range stored {
		file := files[part.FileID]
		if file == nil {
			parts = append(parts, contentPart{Type: "text",
				Text: fmt.Sprintf("[附件 #%d 已删除]", part.FileID)})
			continue
		}
		payload, err := model.ReadChatFilePayload(file)
		if err != nil {
			return nil, fmt.Errorf("读取附件 %s 失败: %w", file.Name, err)
		}
		if file.Kind == model.ChatFileKindImage {
			mimeType := http.DetectContentType(payload)
			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType,
				base64.StdEncoding.EncodeToString(payload))
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &struct {
				URL string `json:"url"`
			}{URL: dataURL}})
		} else {
			parts = append(parts, contentPart{Type: "text",
				Text: fmt.Sprintf("[文件 %s]\n%s", file.Name, string(payload))})
		}
	}
	if len(parts) == 0 {
		return msg.Content, nil
	}
	return parts, nil
}

// historyToWire converts stored messages into OpenAI chat format, returning
// items in history order.
func historyToWire(history []*model.ChatMessage, files map[int]*model.ChatFile, modelName string) ([]historyItem, error) {
	items := make([]historyItem, 0, len(history))
	for _, row := range history {
		item := historyItem{row: row}
		content, err := resolveParts(row, files)
		if err != nil {
			return nil, err
		}
		item.msg = openAIMessage{Role: row.Role, Content: content, ToolCallID: row.ToolCallId, Name: row.Name}
		if row.ToolCalls != nil && *row.ToolCalls != "" {
			var calls []openAIToolCall
			if err := json.Unmarshal([]byte(*row.ToolCalls), &calls); err != nil {
				return nil, fmt.Errorf("解析历史工具调用失败: %w", err)
			}
			item.msg.ToolCalls = calls
		}
		raw, err := json.Marshal(item.msg)
		if err != nil {
			return nil, err
		}
		item.tokens = estimateText(string(raw), modelName)
		items = append(items, item)
	}
	return items, nil
}

// systemContent assembles the system message: session prompt plus selected
// skill instructions.
func systemContent(session *model.ChatSession, skills []*model.ChatSkill) string {
	var parts []string
	if strings.TrimSpace(session.SystemPrompt) != "" {
		parts = append(parts, strings.TrimSpace(session.SystemPrompt))
	}
	for _, skill := range skills {
		if strings.TrimSpace(skill.Instructions) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("## Skill: %s\n%s", skill.Name, strings.TrimSpace(skill.Instructions)))
	}
	return strings.Join(parts, "\n\n")
}

// compactIfNeeded compresses older history into the session summary when the
// estimated context exceeds the budget, returning the working history.
func (r *Runtime) compactIfNeeded(ctx context.Context, emit Emitter, turn TurnRequest, tools *toolSet) ([]*model.ChatMessage, error) {
	budget := turn.ContextBudget
	if budget <= 0 {
		budget = config.ChatContextBudgetTokens
	}
	if budget <= 0 {
		return turn.History, nil
	}
	items, err := historyToWire(turn.History, turn.Files, turn.Session.Model)
	if err != nil {
		return nil, err
	}
	systemTokens := estimateText(systemContent(turn.Session, turn.Skills), turn.Session.Model)
	toolTokens := 0
	for _, def := range tools.defs {
		raw, _ := json.Marshal(def)
		toolTokens += estimateText(string(raw), turn.Session.Model)
	}
	total := systemTokens + toolTokens + estimateText(turn.Session.Summary, turn.Session.Model)
	for _, item := range items {
		total += item.tokens
	}
	if total <= budget {
		return turn.History, nil
	}

	// Keep the trailing messages whose estimate fits in two fifths of the
	// budget; the kept side always retains the newest message and the
	// summarized side keeps at least one.
	split := len(items)
	for split > 1 && keptTokens(items[split-1:]) <= budget*2/5 {
		split--
	}
	if split >= len(items) {
		split = len(items) - 1
	}
	if split <= 0 {
		return turn.History, nil
	}
	older, keptItems := items[:split], items[split:]

	summary, err := r.summarize(ctx, turn, older)
	if err != nil {
		// Compaction is an optimization; an upstream failure should not kill
		// the turn, which proceeds with the uncompressed history.
		return turn.History, nil
	}
	turn.Session.Summary = summary
	turn.Session.CompactedThrough = older[len(older)-1].row.Id
	if err := model.UpdateChatSession(turn.Session); err != nil {
		return turn.History, nil
	}
	emit(Event{Type: EventCompact, Content: summary})
	working := make([]*model.ChatMessage, 0, len(keptItems))
	for _, item := range keptItems {
		working = append(working, item.row)
	}
	return working, nil
}

// keptTokens sums the estimates of the given items.
func keptTokens(items []historyItem) int {
	sum := 0
	for _, item := range items {
		sum += item.tokens
	}
	return sum
}

// summarize renders the older transcript and compresses it through one
// summarizer leg on the session's model.
func (r *Runtime) summarize(ctx context.Context, turn TurnRequest, older []historyItem) (string, error) {
	var transcript strings.Builder
	if turn.Session.Summary != "" {
		transcript.WriteString("[此前摘要]\n")
		transcript.WriteString(turn.Session.Summary)
		transcript.WriteString("\n\n[新增对话]\n")
	}
	for _, item := range older {
		text := item.row.Content
		if len(text) > compactTranscriptChars {
			text = text[:compactTranscriptChars] + "…"
		}
		if strings.TrimSpace(text) == "" && len(item.msg.ToolCalls) == 0 {
			continue
		}
		transcript.WriteString(fmt.Sprintf("[%s]\n%s\n\n", item.row.Role, text))
		for _, call := range item.msg.ToolCalls {
			transcript.WriteString(fmt.Sprintf("[assistant 调用工具 %s]\n%s\n\n", call.Function.Name, call.Function.Arguments))
		}
	}
	body := map[string]any{
		"model": turn.Session.Model,
		"messages": []openAIMessage{
			{Role: "system", Content: compactPrompt},
			{Role: "user", Content: transcript.String()},
		},
		"stream":      true,
		"max_tokens":  compactSummarizerTokens,
		"temperature": 0.1,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	result, err := turn.Leg.RunLeg(ctx, LegRequest{Body: raw, MaxOutputTokens: compactSummarizerTokens}, nil)
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(result.Content)
	if summary == "" {
		return "", fmt.Errorf("摘要为空")
	}
	return summary, nil
}
