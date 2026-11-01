package replicate

import (
	"encoding/json"
	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamPlainTextAndTerminalFailure(t *testing.T) {
	for _, reason := range []string{"", "canceled", "error"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "event: output\ndata:  \"quoted\"\n\nevent: done\ndata: {\"reason\":\""+reason+"\"}\n\n")
		}))
		recorder := httptest.NewRecorder()
		ctx := adaptor.NewContext(httptest.NewRequest("POST", "/", nil), recorder)
		text, err := streamPredictionOutput(ctx, &meta.Meta{IsStream: true, ActualModelName: "test"}, srv.URL)
		srv.Close()
		if text != " \"quoted\"" {
			t.Fatal(text)
		}
		if (reason != "") != (err != nil) {
			t.Fatalf("reason %s err %v", reason, err)
		}
		if reason != "" && strings.Contains(recorder.Body.String(), "[DONE]") {
			t.Fatal("failure reported as success")
		}
	}
}

func TestNativeModelInputAndPredictionShapes(t *testing.T) {
	for _, body := range []string{`{"replicate_input":{"prompt":"hello","new_model_option":{"x":1},"steps":87}}`} {
		request, err := ConvertChatRequest(&model.ConversionInput{RawBody: []byte(body)})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"new_model_option":{"x":1}`) || !strings.Contains(string(raw), `"stream":true`) {
			t.Fatal(string(raw))
		}
	}
	for _, raw := range []string{`{"output":"hello"}`, `{"output":["he","llo"]}`} {
		var prediction ChatResponse
		if err := json.Unmarshal([]byte(raw), &prediction); err != nil {
			t.Fatal(err)
		}
		if strings.Join(prediction.Output, "") != "hello" {
			t.Fatal(prediction.Output)
		}
	}
	a := &Adaptor{}
	converted, err := a.ConvertImageRequest(&model.ImageRequest{Prompt: "hello", N: 1, ReplicateInput: map[string]any{"prompt": "hello", "new_option": true}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(converted)
	if string(raw) != `{"input":{"new_option":true,"prompt":"hello"}}` {
		t.Fatal(string(raw))
	}
}
