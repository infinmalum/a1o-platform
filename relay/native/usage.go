package native

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// Usage contains the provider-reported token counts. Seen is false when no
// usage report was received, so callers can choose a conservative fallback.
type Usage struct {
	Input  int64
	Output int64
	// CacheRead, CacheCreation, and Reasoning carry the provider's token
	// breakdown. Input includes cache reads and writes.
	CacheRead       int64
	CacheCreation   int64
	CacheCreation1h int64 `json:",omitempty"`
	Reasoning       int64
	Seen            bool
	Partial         bool `json:",omitempty"`
	// Complete marks a protocol-level end event where one exists.
	Complete bool
}

type AnthropicUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreation            struct {
		FiveMinutes int64 `json:"ephemeral_5m_input_tokens"`
		OneHour     int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func (report AnthropicUsage) Apply(u *Usage) bool {
	if min(report.InputTokens, report.OutputTokens, report.CacheCreationInputTokens,
		report.CacheReadInputTokens, report.CacheCreation.FiveMinutes, report.CacheCreation.OneHour) < 0 {
		return false
	}
	uncached := max(u.Input-u.CacheRead-u.CacheCreation, report.InputTokens)
	u.CacheRead = max(u.CacheRead, report.CacheReadInputTokens)
	u.CacheCreation = max(u.CacheCreation, report.CacheCreationInputTokens, report.CacheCreation.FiveMinutes+report.CacheCreation.OneHour)
	u.CacheCreation1h = max(u.CacheCreation1h, report.CacheCreation.OneHour)
	u.Input = uncached + u.CacheCreation + u.CacheRead
	u.Output = max(u.Output, report.OutputTokens)
	u.Seen = true
	return true
}

type geminiUsage struct {
	PromptTokenCount     int64 `json:"promptTokenCount"`
	CandidatesTokenCount int64 `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int64 `json:"thoughtsTokenCount"`
	TotalTokenCount      int64 `json:"totalTokenCount"`
	CachedTokenCount     int64 `json:"cachedContentTokenCount"`
}

type openAIUsage struct {
	InputTokens   int64              `json:"input_tokens"`
	OutputTokens  int64              `json:"output_tokens"`
	InputDetails  *openAIUsageDetail `json:"input_tokens_details"`
	OutputDetails *openAIUsageDetail `json:"output_tokens_details"`
}

type openAIUsageDetail struct {
	CachedTokens    int64 `json:"cached_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

func (u *Usage) observe(protocol Protocol, data []byte, gemini *geminiStreamProgress) {
	switch protocol {
	case Anthropic:
		var result struct {
			Type    string          `json:"type"`
			Usage   *AnthropicUsage `json:"usage"`
			Message *struct {
				Usage *AnthropicUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(data, &result) != nil {
			return
		}
		if result.Type == "message_stop" {
			u.Complete = true
		}
		usage := result.Usage
		if result.Message != nil && result.Message.Usage != nil {
			usage = result.Message.Usage
		}
		if usage == nil {
			return
		}
		usage.Apply(u)
	case Gemini:
		var result struct {
			Usage      *geminiUsage `json:"usageMetadata"`
			Candidates []struct {
				Index        int    `json:"index"`
				FinishReason string `json:"finishReason"`
			} `json:"candidates"`
			PromptFeedback *struct {
				BlockReason string `json:"blockReason"`
			} `json:"promptFeedback"`
		}
		if json.Unmarshal(data, &result) != nil {
			return
		}
		if gemini != nil {
			if result.PromptFeedback != nil && result.PromptFeedback.BlockReason != "" &&
				result.PromptFeedback.BlockReason != "BLOCK_REASON_UNSPECIFIED" {
				gemini.blocked = true
			}
			for _, candidate := range result.Candidates {
				finished := candidate.FinishReason != "" && candidate.FinishReason != "FINISH_REASON_UNSPECIFIED"
				gemini.finished[candidate.Index] = gemini.finished[candidate.Index] || finished
			}
			u.Complete = gemini.blocked || len(gemini.finished) > 0
			for _, finished := range gemini.finished {
				u.Complete = u.Complete && finished
			}
			if len(result.Candidates) > 0 && !u.Complete {
				gemini.finalUsage = false
			}
			if result.Usage != nil && u.Complete {
				gemini.finalUsage = true
			}
		}
		if result.Usage == nil {
			return
		}
		u.Input = max(u.Input, result.Usage.PromptTokenCount)
		output := result.Usage.CandidatesTokenCount + result.Usage.ThoughtsTokenCount
		if result.Usage.TotalTokenCount > result.Usage.PromptTokenCount {
			output = max(output, result.Usage.TotalTokenCount-result.Usage.PromptTokenCount)
		}
		u.Output = max(u.Output, output)
		u.CacheRead = max(u.CacheRead, result.Usage.CachedTokenCount)
		u.Reasoning = max(u.Reasoning, result.Usage.ThoughtsTokenCount)
		u.Seen = true
	case OpenAIResponses, OpenResponses:
		var result struct {
			Type     string       `json:"type"`
			Usage    *openAIUsage `json:"usage"`
			Response *struct {
				Usage *openAIUsage `json:"usage"`
			} `json:"response"`
		}
		if json.Unmarshal(data, &result) != nil {
			return
		}
		switch result.Type {
		case "response.completed", "response.failed", "response.incomplete":
			u.Complete = true
		}
		usage := result.Usage
		if result.Response != nil && result.Response.Usage != nil {
			usage = result.Response.Usage
		}
		if usage != nil {
			u.Input = max(u.Input, usage.InputTokens)
			u.Output = max(u.Output, usage.OutputTokens)
			if usage.InputDetails != nil {
				u.CacheRead = max(u.CacheRead, usage.InputDetails.CachedTokens)
			}
			if usage.OutputDetails != nil {
				u.Reasoning = max(u.Reasoning, usage.OutputDetails.ReasoningTokens)
			}
			u.Seen = true
		}
	case SystemOne:
		var result struct {
			Usage *openAIUsage `json:"usage"`
		}
		if json.Unmarshal(data, &result) == nil && result.Usage != nil {
			u.Input = max(u.Input, result.Usage.InputTokens)
			u.Output = max(u.Output, result.Usage.OutputTokens)
			u.Seen = true
		}
	case OpenAIChat:
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			u.Complete = true
			return
		}
		var result struct {
			Usage *struct {
				PromptTokens         int64              `json:"prompt_tokens"`
				CompletionTokens     int64              `json:"completion_tokens"`
				PromptCacheHitTokens int64              `json:"prompt_cache_hit_tokens"`
				PromptDetails        *openAIUsageDetail `json:"prompt_tokens_details"`
				CompletionDetails    *openAIUsageDetail `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &result) == nil && result.Usage != nil {
			u.Input = max(u.Input, result.Usage.PromptTokens)
			u.Output = max(u.Output, result.Usage.CompletionTokens)
			u.CacheRead = max(u.CacheRead, result.Usage.PromptCacheHitTokens)
			if result.Usage.PromptDetails != nil {
				u.CacheRead = max(u.CacheRead, result.Usage.PromptDetails.CachedTokens)
			}
			if result.Usage.CompletionDetails != nil {
				u.Reasoning = max(u.Reasoning, result.Usage.CompletionDetails.ReasoningTokens)
			}
			u.Seen = true
		}
	case OpenAIEmbeddings:
		var result struct {
			Usage *struct {
				PromptTokens int64 `json:"prompt_tokens"`
				TotalTokens  int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &result) == nil && result.Usage != nil {
			u.Input = max(result.Usage.PromptTokens, result.Usage.TotalTokens)
			u.Seen = true
		}
	case OpenAIModerations:
		var result struct {
			Usage *struct {
				InputTokens  int64 `json:"input_tokens"`
				PromptTokens int64 `json:"prompt_tokens"`
				TotalTokens  int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &result) == nil && result.Usage != nil {
			u.Input = max(result.Usage.InputTokens, result.Usage.PromptTokens, result.Usage.TotalTokens)
			u.Seen = true
		}
	}
}

// CopyResponse forwards provider bytes unchanged while observing usage. It
// flushes SSE chunks as they arrive and never reconstructs provider events.
func CopyResponse(dst http.ResponseWriter, src io.Reader, protocol Protocol, stream bool) (Usage, error) {
	var usage Usage
	if !stream {
		return copyJSONResponse(dst, src, protocol)
	}
	observer := &sseObserver{protocol: protocol, usage: &usage}
	if protocol == Gemini {
		observer.gemini = &geminiStreamProgress{finished: make(map[int]bool)}
	}
	_, err := io.Copy(&observingWriter{dst: dst, observer: observer}, src)
	if observer.gemini != nil {
		usage.Partial = usage.Seen && !observer.gemini.finalUsage
	}
	return usage, err
}

type errorRecordingWriter struct {
	dst http.ResponseWriter
	err error
}

type errorRecordingReader struct {
	src io.Reader
	err error
}

func (r *errorRecordingReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if err != nil && err != io.EOF {
		r.err = err
	}
	return n, err
}

func (w *errorRecordingWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.dst.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

// copyJSONResponse decodes only top-level usage while forwarding the entire
// response. Large embedding arrays therefore do not need to be buffered.
// ReadJSONUsage observes counters separately from response conversion.
func ReadJSONUsage(protocol Protocol, body []byte) Usage {
	var usage Usage
	if json.Valid(body) {
		usage.observe(protocol, body, nil)
		usage.Complete = usage.Seen
	}
	return usage
}

func copyJSONResponse(dst http.ResponseWriter, src io.Reader, protocol Protocol) (Usage, error) {
	var usage Usage
	writer := &errorRecordingWriter{dst: dst}
	reader := &errorRecordingReader{src: src}
	tee := io.TeeReader(reader, writer)
	decoder := json.NewDecoder(tee)
	if token, err := decoder.Token(); err == nil && token == json.Delim('{') {
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				break
			}
			name, ok := key.(string)
			if !ok {
				break
			}
			if name == "usage" || name == "usageMetadata" {
				var raw json.RawMessage
				if decoder.Decode(&raw) != nil {
					break
				}
				wrapped := append(append([]byte(`{"`+name+`":`), raw...), '}')
				usage.observe(protocol, wrapped, nil)
			} else if skipJSONValue(decoder) != nil {
				break
			}
		}
	}
	_, err := io.Copy(io.Discard, tee)
	if writer.err != nil {
		err = writer.err
	} else if reader.err != nil {
		err = reader.err
	}
	usage.Complete = err == nil
	return usage, err
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return nil
	}
	depth := 1
	for depth > 0 {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		if nested, ok := token.(json.Delim); ok {
			switch nested {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

type observingWriter struct {
	dst      http.ResponseWriter
	observer *sseObserver
}

func (w *observingWriter) Write(p []byte) (int, error) {
	w.observer.write(p)
	n, err := w.dst.Write(p)
	if flusher, ok := w.dst.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

type sseObserver struct {
	protocol   Protocol
	usage      *Usage
	gemini     *geminiStreamProgress
	line       []byte
	data       []byte
	discarding bool
}

type geminiStreamProgress struct {
	finished   map[int]bool
	blocked    bool
	finalUsage bool
}

const maxObservedEvent = 2 << 20

func (o *sseObserver) write(p []byte) {
	for len(p) > 0 {
		index := bytes.IndexByte(p, '\n')
		if index < 0 {
			o.appendLine(p)
			return
		}
		o.appendLine(p[:index])
		if !o.discarding {
			o.completeLine()
		} else {
			o.line = nil
			o.data = nil
			o.discarding = false
		}
		p = p[index+1:]
	}
}

func (o *sseObserver) appendLine(p []byte) {
	if o.discarding {
		return
	}
	if len(o.line)+len(p) > maxObservedEvent {
		o.discarding = true
		o.line = nil
		o.data = nil
		return
	}
	o.line = append(o.line, p...)
}

func (o *sseObserver) completeLine() {
	line := bytes.TrimSuffix(o.line, []byte{'\r'})
	if len(line) == 0 {
		if len(o.data) > 0 {
			o.usage.observe(o.protocol, o.data, o.gemini)
		}
		o.data = nil
	} else if bytes.HasPrefix(line, []byte("data:")) {
		value := bytes.TrimPrefix(line, []byte("data:"))
		value = bytes.TrimPrefix(value, []byte(" "))
		if len(o.data)+len(value)+1 <= maxObservedEvent {
			if len(o.data) > 0 {
				o.data = append(o.data, '\n')
			}
			o.data = append(o.data, value...)
		} else {
			o.data = nil
		}
	}
	o.line = nil
}
