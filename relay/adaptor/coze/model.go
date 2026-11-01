package coze

// Wire types of the Coze v3 chat API (https://api.coze.com/v3/chat and
// api.coze.cn/v3/chat, verified against the coze-studio API reference). The
// upstream always streams: the gateway sets stream=true and aggregates the
// event stream for synchronous clients.

// Request is the v3 chat request. All conversation input travels through
// additional_messages; there is no separate query field.
type Request struct {
	BotID           string    `json:"bot_id"`
	UserID          string    `json:"user_id"`
	Stream          bool      `json:"stream"`
	AutoSaveHistory bool      `json:"auto_save_history"`
	AdditionalMsgs  []Message `json:"additional_messages"`
}

// Message is one v3 EnterMessage. Bots accept user and assistant text
// messages; content_type text carries plain text.
type Message struct {
	Role        string `json:"role"`
	Type        string `json:"type,omitempty"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
}

// message types on the event stream. Only answer messages carry user-visible
// content; verbose and follow_up messages are bot metadata.
const (
	messageTypeAnswer   = "answer"
	messageTypeVerbose  = "verbose"
	messageTypeFollowUp = "follow_up"
)

// StreamEvent is one v3 SSE event. The event name rides the SSE event line;
// Data carries the typed payload.
type StreamEvent struct {
	Event string
	Data  []byte
}

// chatEvent is the payload of conversation.chat.* events.
type chatEvent struct {
	ID               string      `json:"id"`
	Status           string      `json:"status"`
	Code             int         `json:"code"`
	Msg              string      `json:"msg"`
	LastError        *errorEvent `json:"last_error,omitempty"`
	LastErrorMessage string      `json:"last_error_msg"`
	Usage            *struct {
		TokenCount  int64 `json:"token_count"`
		OutputCount int64 `json:"output_count"`
		InputCount  int64 `json:"input_count"`
	} `json:"usage"`
}

// messageEvent is the payload of conversation.message.* events.
type messageEvent struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Type        string `json:"type"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
}

// errorEvent is the payload of the error event.
type errorEvent struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}
