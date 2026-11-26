package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/infinmalum/a1o-platform/relay/sse"
)

// prepareCodeAssistBody wraps a Gemini generateContent payload into the Code
// Assist envelope: the model and the bound cloudaicompanion project move out
// of the inner request into the envelope fields.
func prepareCodeAssistBody(body []byte, model, project string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, errors.New("invalid Gemini JSON request")
	}
	delete(fields, "model")
	inner, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	envelope := map[string]json.RawMessage{
		"model":   []byte(`"` + model + `"`),
		"project": []byte(`"` + project + `"`),
		"request": inner,
	}
	return json.Marshal(envelope)
}

// NormalizeCodeAssistResponse keeps provider decoding at the transport
// boundary so the existing downstream bridges, accounting and error handling
// are shared: Code Assist wraps every GenerateContent response in a
// `{"response": ...}` envelope, on the JSON body and on every SSE frame.
func NormalizeCodeAssistResponse(resp *http.Response, input Request) *http.Response {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp
	}
	if input.Stream && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		source := resp.Body
		reader, writer := io.Pipe()
		resp.Body = &codeAssistStreamBody{PipeReader: reader, source: source}
		go func() {
			defer source.Close()
			err := sse.Read(source, func(e sse.Event) error {
				payload, unwrapErr := unwrapCodeAssist([]byte(e.Data))
				if unwrapErr != nil {
					return unwrapErr
				}
				_, writeErr := writer.Write(appendSSEFrame(payload))
				return writeErr
			})
			_ = writer.CloseWithError(err)
		}()
		return resp
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err == nil {
		body, err = unwrapCodeAssist(body)
	}
	if err != nil {
		resp.StatusCode = http.StatusBadGateway
		body, _ = json.Marshal(map[string]any{"error": map[string]string{"message": err.Error(), "type": "code_assist_error"}})
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.Header.Set("Content-Type", "application/json")
	resp.ContentLength = int64(len(body))
	return resp
}

type codeAssistStreamBody struct {
	*io.PipeReader
	source io.ReadCloser
}

func (b *codeAssistStreamBody) Close() error {
	_ = b.source.Close()
	return b.PipeReader.Close()
}

// unwrapCodeAssist returns the inner GenerateContent payload, passing
// payloads through unchanged when they carry no envelope.
func unwrapCodeAssist(body []byte) ([]byte, error) {
	var envelope struct {
		Response json.RawMessage `json:"response"`
		Error    *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Response == nil {
		if err != nil {
			return nil, errors.New("invalid Code Assist response JSON")
		}
		if envelope.Error != nil {
			return nil, errors.New("Code Assist failed: " + envelope.Error.Message)
		}
		return body, nil
	}
	return envelope.Response, nil
}
