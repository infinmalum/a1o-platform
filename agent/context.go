package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	openaiadapter "github.com/infinmalum/a1o-platform/relay/adaptor/openai"
)

const (
	// compactSummarizerTokens bounds the summarization leg's output.
	compactSummarizerTokens = int64(1536)
	// compactTranscriptChars caps one message's render inside the transcript.
	compactTranscriptChars = 4000
	// compactSummarizerTemperature keeps the summarizer leg conservative.
	compactSummarizerTemperature = 0.1
)

// storedToolCall mirrors the persisted ChatMessage.ToolCalls wire form.
type storedToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// storedReasoning is the JSON blob persisted on assistant rows: the turn's
// opaque native state so the next call replays it to the bound channel.
type storedReasoning struct {
	Summary           []SummaryPart     `json:"summary,omitempty"`
	Content           []ContentPart     `json:"content,omitempty"`
	Signature         string            `json:"signature,omitempty"`
	EncryptedContent  string            `json:"encrypted_content,omitempty"`
	ThoughtSignatures map[string]string `json:"thought_signatures,omitempty"`
}

// historyEntry couples one stored message with its standard Items and token
// estimate.
type historyEntry struct {
	row    *model.ChatMessage
	items  []Item
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

// resolveParts expands a user message's stored attachments into standard
// content parts, inlining text payloads and converting images to data URLs.
func resolveParts(msg *model.ChatMessage, files map[int]*model.ChatFile) ([]ContentPart, error) {
	if msg.Parts == nil || *msg.Parts == "" {
		if msg.Content == "" {
			return nil, nil
		}
		return []ContentPart{{Type: PartInputText, Text: msg.Content}}, nil
	}
	var stored []model.ChatMessagePart
	if err := json.Unmarshal([]byte(*msg.Parts), &stored); err != nil {
		if msg.Content == "" {
			return nil, nil
		}
		return []ContentPart{{Type: PartInputText, Text: msg.Content}}, nil
	}
	parts := make([]ContentPart, 0, len(stored)+1)
	if msg.Content != "" {
		parts = append(parts, ContentPart{Type: PartInputText, Text: msg.Content})
	}
	for _, part := range stored {
		file := files[part.FileID]
		if file == nil {
			parts = append(parts, ContentPart{Type: PartInputText,
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
			parts = append(parts, ContentPart{Type: PartInputImage, ImageURL: dataURL})
		} else {
			parts = append(parts, ContentPart{Type: PartInputText,
				Text: fmt.Sprintf("[文件 %s]\n%s", file.Name, string(payload))})
		}
	}
	return parts, nil
}

// historyToItems converts stored messages into standard Items in history
// order. Assistant rows expand to their reasoning item (when one was
// carried), the message item, and their function-call items so the next call
// replays tool calls and opaque state in the shapes the bridges expect.
func historyToItems(history []*model.ChatMessage, files map[int]*model.ChatFile, modelName string) ([]historyEntry, error) {
	entries := make([]historyEntry, 0, len(history))
	for _, row := range history {
		entry := historyEntry{row: row}
		var reasoning *storedReasoning
		if row.Reasoning != nil && *row.Reasoning != "" {
			reasoning = &storedReasoning{}
			if err := json.Unmarshal([]byte(*row.Reasoning), reasoning); err != nil {
				return nil, fmt.Errorf("解析历史思考状态失败: %w", err)
			}
		}
		switch row.Role {
		case model.ChatRoleUser:
			parts, err := resolveParts(row, files)
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 {
				continue
			}
			entry.items = append(entry.items, Item{Type: ItemMessage, Role: "user", Content: parts})
		case model.ChatRoleAssistant:
			if reasoning != nil {
				reasoningItem := Item{Type: ItemReasoning, Summary: reasoning.Summary,
					Content: reasoning.Content, Signature: reasoning.Signature,
					EncryptedContent: reasoning.EncryptedContent}
				entry.items = append(entry.items, reasoningItem)
			}
			var calls []storedToolCall
			if row.ToolCalls != nil && *row.ToolCalls != "" {
				if err := json.Unmarshal([]byte(*row.ToolCalls), &calls); err != nil {
					return nil, fmt.Errorf("解析历史工具调用失败: %w", err)
				}
			}
			if row.Content != "" {
				entry.items = append(entry.items, Item{Type: ItemMessage, Role: "assistant",
					Content: []ContentPart{{Type: PartOutputText, Text: row.Content}}})
			}
			for _, call := range calls {
				item := Item{Type: ItemFunctionCall, CallID: call.ID,
					Name: call.Function.Name, Arguments: call.Function.Arguments}
				if reasoning != nil {
					item.ThoughtSignature = reasoning.ThoughtSignatures[call.ID]
				}
				entry.items = append(entry.items, item)
			}
		case model.ChatRoleTool:
			if row.ToolCallId == "" {
				continue
			}
			entry.items = append(entry.items, Item{Type: ItemFunctionCallOutput,
				CallID: row.ToolCallId, Output: row.Content})
		}
		if len(entry.items) == 0 {
			continue
		}
		raw, err := json.Marshal(entry.items)
		if err != nil {
			return nil, err
		}
		entry.tokens = estimateText(string(raw), modelName)
		entries = append(entries, entry)
	}
	return entries, nil
}

// systemContent assembles the run instructions: the frozen agent
// instructions when the run pins an agent version, then the session prompt,
// then the selected skill instructions.
func systemContent(session *model.ChatSession, skills []*model.ChatSkill, spec *model.AgentVersionSpec) string {
	var parts []string
	if spec != nil && strings.TrimSpace(spec.Instructions) != "" {
		parts = append(parts, strings.TrimSpace(spec.Instructions))
	}
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
func (e *Engine) compactIfNeeded(ctx context.Context, emit Emitter, turn ChatTurnRequest, tools *toolSet) ([]*model.ChatMessage, error) {
	budget := turn.ContextBudget
	if budget <= 0 {
		budget = config.ChatContextBudgetTokens
	}
	if budget <= 0 {
		return turn.History, nil
	}
	entries, err := historyToItems(turn.History, turn.Files, turn.Session.Model)
	if err != nil {
		return nil, err
	}
	systemTokens := estimateText(systemContent(turn.Session, turn.Skills, turn.AgentSpec), "")
	toolTokens := 0
	for _, def := range tools.defs {
		raw, _ := json.Marshal(def)
		toolTokens += estimateText(string(raw), "")
	}
	total := systemTokens + toolTokens + estimateText(turn.Session.Summary, "")
	for _, entry := range entries {
		total += entry.tokens
	}
	if total <= budget {
		return turn.History, nil
	}

	// Keep the trailing messages whose estimate fits in two fifths of the
	// budget; the kept side always retains the newest message and the
	// summarized side keeps at least one.
	split := len(entries)
	for split > 1 && keptTokens(entries[split-1:]) <= budget*2/5 {
		split--
	}
	if split >= len(entries) {
		split = len(entries) - 1
	}
	if split <= 0 {
		return turn.History, nil
	}
	older, keptEntries := entries[:split], entries[split:]

	summary, err := e.summarize(ctx, turn, older)
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
	// Emission failure means the client is gone; the request context aborts
	// the turn at the next checkpoint.
	_ = e.emitEvent(turn.Run, 0, model.RunEventContextCompacted,
		map[string]any{"content": summary}, true, emit)
	working := make([]*model.ChatMessage, 0, len(keptEntries))
	for _, entry := range keptEntries {
		working = append(working, entry.row)
	}
	return working, nil
}

// keptTokens sums the estimates of the given entries.
func keptTokens(entries []historyEntry) int {
	sum := 0
	for _, entry := range entries {
		sum += entry.tokens
	}
	return sum
}

// summarize renders the older transcript and compresses it through one
// summarizer leg on the session's model. The leg bills through the same run
// but never enters the run context.
func (e *Engine) summarize(ctx context.Context, turn ChatTurnRequest, older []historyEntry) (string, error) {
	var transcript strings.Builder
	if turn.Session.Summary != "" {
		transcript.WriteString("[此前摘要]\n")
		transcript.WriteString(turn.Session.Summary)
		transcript.WriteString("\n\n[新增对话]\n")
	}
	for _, entry := range older {
		row := entry.row
		text := row.Content
		if len(text) > compactTranscriptChars {
			text = text[:compactTranscriptChars] + "…"
		}
		var calls []storedToolCall
		if row.ToolCalls != nil && *row.ToolCalls != "" {
			_ = json.Unmarshal([]byte(*row.ToolCalls), &calls)
		}
		if strings.TrimSpace(text) == "" && len(calls) == 0 {
			continue
		}
		transcript.WriteString(fmt.Sprintf("[%s]\n%s\n\n", row.Role, text))
		for _, call := range calls {
			transcript.WriteString(fmt.Sprintf("[assistant 调用工具 %s]\n%s\n\n", call.Function.Name, call.Function.Arguments))
		}
	}
	temperature := compactSummarizerTemperature
	result, err := turn.Executor.Invoke(ctx, InvokeRequest{
		RunID:             turn.Run.Id,
		Model:             turn.Session.Model,
		Instructions:      compactPrompt,
		Items:             []Item{{Type: ItemMessage, Role: "user", Content: []ContentPart{{Type: PartInputText, Text: transcript.String()}}}},
		MaxOutputTokens:   compactSummarizerTokens,
		Temperature:       &temperature,
		SkipContextAppend: true,
	})
	if err != nil {
		return "", err
	}
	summary := messageText(result.Items)
	if strings.TrimSpace(summary) == "" {
		return "", fmt.Errorf("摘要为空")
	}
	return summary, nil
}

// messageText joins the output_text parts of a result's message items.
func messageText(items []Item) string {
	var text string
	for _, item := range items {
		if item.Type != ItemMessage {
			continue
		}
		for _, part := range item.Content {
			if part.Type == PartOutputText {
				text += part.Text
			}
		}
	}
	return text
}
