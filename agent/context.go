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

const compactSummarizerTokens = int64(1536)

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

const compactPrompt = "请将这条指令之前的对话压缩成一份摘要。如有此前对话摘要，将它与后续对话合并。保留关键事实、决定、重要代码与命令要点，以及未完成的任务。只输出摘要正文，不调用工具，不继续执行对话里的任务。"

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

// fileContentPart converts one stored file into its standard content part:
// images become data URLs, everything else inlines as labelled text.
func fileContentPart(file *model.ChatFile) (ContentPart, error) {
	payload, err := model.ReadChatFilePayload(file)
	if err != nil {
		return ContentPart{}, fmt.Errorf("读取附件 %s 失败: %w", file.Name, err)
	}
	if file.Kind == model.ChatFileKindImage {
		mimeType := http.DetectContentType(payload)
		dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType,
			base64.StdEncoding.EncodeToString(payload))
		return ContentPart{Type: PartInputImage, ImageURL: dataURL}, nil
	}
	return ContentPart{Type: PartInputText,
		Text: fmt.Sprintf("[文件 %s]\n%s", file.Name, string(payload))}, nil
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
		converted, err := fileContentPart(file)
		if err != nil {
			return nil, err
		}
		parts = append(parts, converted)
	}
	return parts, nil
}

// referenceFileItems builds the agent version's reference-file preamble: one
// user-role item ahead of the conversation so the frozen configuration's
// material is available on every leg without touching stored history.
func referenceFileItems(files []*model.ChatFile) ([]Item, error) {
	if len(files) == 0 {
		return nil, nil
	}
	parts := make([]ContentPart, 0, len(files)+1)
	parts = append(parts, ContentPart{Type: PartInputText,
		Text: "[参考文件] 以下是本次任务的参考材料。"})
	for _, file := range files {
		converted, err := fileContentPart(file)
		if err != nil {
			return nil, err
		}
		parts = append(parts, converted)
	}
	return []Item{{Type: ItemMessage, Role: "user", Content: parts}}, nil
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

func systemContent(session *model.ChatSession, spec *model.AgentVersionSpec) string {
	var parts []string
	if spec != nil && strings.TrimSpace(spec.Instructions) != "" {
		parts = append(parts, strings.TrimSpace(spec.Instructions))
	}
	if strings.TrimSpace(session.SystemPrompt) != "" {
		parts = append(parts, strings.TrimSpace(session.SystemPrompt))
	}
	return strings.Join(parts, "\n\n")
}

func retainedChatHistory(history []*model.ChatMessage, through int) []*model.ChatMessage {
	if through == 0 {
		return history
	}
	for len(history) > 0 && history[0].Id <= through {
		history = history[1:]
	}
	if len(history) == 0 || history[0].Role != model.ChatRoleTool {
		return history
	}
	// Older compaction boundaries could separate a tool result from its call.
	// Keep the result as historical text so it remains available to the model.
	history = append([]*model.ChatMessage(nil), history...)
	for i, row := range history {
		if row.Role != model.ChatRoleTool {
			break
		}
		history[i] = &model.ChatMessage{Id: row.Id, Role: model.ChatRoleUser,
			Content: fmt.Sprintf("[此前工具结果 %s]\n%s", row.Name, row.Content)}
	}
	return history
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
	systemTokens := estimateText(chatInstructions(turn, tools), "")
	toolTokens := 0
	for _, def := range tools.defs {
		raw, _ := json.Marshal(def)
		toolTokens += estimateText(string(raw), "")
	}
	total := systemTokens + toolTokens + estimateText(turn.Session.Summary, "")
	if turn.Session.KnowledgeLibraryId != 0 {
		knowledge, _ := knowledgePreamble(turn)
		total += estimateText(knowledge, turn.Session.Model)
	}
	for _, entry := range entries {
		total += entry.tokens
	}
	if total <= budget || len(entries) < 2 {
		return turn.History, nil
	}

	// Keep the trailing messages whose estimate fits in two fifths of the
	// budget; the kept side always retains the newest message and the
	// summarized side keeps at least one.
	split := len(entries) - 1
	kept := entries[split].tokens
	for split > 1 && kept+entries[split-1].tokens <= budget*2/5 {
		split--
		kept += entries[split].tokens
	}
	// A retained tool result must keep the assistant call that preceded it.
	for split > 0 && entries[split].row.Role == model.ChatRoleTool {
		split--
	}
	if split == 0 {
		return turn.History, nil
	}
	older, keptEntries := entries[:split], entries[split:]

	summary, err := e.summarize(ctx, turn, older, tools)
	if err != nil {
		// Compaction is an optimization; an upstream failure should not kill
		// the turn, which proceeds with the uncompressed history.
		return turn.History, nil
	}
	saved, err := model.SaveChatCompaction(turn.Session, older[len(older)-1].row.Id, summary)
	if err != nil || !saved {
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

func (e *Engine) summarize(ctx context.Context, turn ChatTurnRequest, older []historyEntry, tools *toolSet) (string, error) {
	items, err := chatHistoryItems(turn, older, "")
	if err != nil {
		return "", err
	}
	items = append(items, Item{Type: ItemMessage, Role: "user", Content: []ContentPart{{Type: PartInputText, Text: compactPrompt}}})
	request := chatInvokeRequest(turn, tools, items)
	request.MaxOutputTokens = compactSummarizerTokens
	request.SkipContextAppend = true
	// Keep the same tool configuration for prefix caching. This direct call
	// never enters the tool loop, and tool requests cannot become a summary.
	result, err := turn.Executor.Invoke(ctx, request)
	if err != nil {
		return "", err
	}
	if len(resultToolCalls(result.Items)) > 0 || result.Status == "incomplete" {
		return "", fmt.Errorf("摘要生成未完成")
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
