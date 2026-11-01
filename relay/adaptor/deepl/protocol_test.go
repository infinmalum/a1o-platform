package deepl

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/relay/adaptor"
	"github.com/infinmalum/a1o-platform/relay/meta"
	"github.com/infinmalum/a1o-platform/relay/model"
)

func TestLanguageParsing(t *testing.T) {
	for input, want := range map[string]string{"EN-US": "EN-US", "deepl-PT-BR": "PT-BR", "DE": "DE", "deepl-de": "DE", "deepl-en-us": "EN-US"} {
		got, err := parseLangFromModelName(input)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", input, got, err)
		}
	}
	for _, input := range []string{"deepl", "", "english"} {
		if _, err := parseLangFromModelName(input); err == nil {
			t.Fatalf("%q must fail instead of defaulting to a language", input)
		}
	}
}

func TestConvertRequestRejectsNonTranslationSemantics(t *testing.T) {
	single := model.TextRequest{Model: "deepl-de", Messages: []model.Message{{Role: "user", Content: "hello"}}}
	request, text, err := ConvertRequest(single)
	if err != nil || text != "hello" || request.TargetLang != "DE" || !request.ShowBilledCharacters {
		t.Fatalf("%+v %q %v", request, text, err)
	}
	rejects := []model.TextRequest{
		{Model: "DE", Messages: []model.Message{{Role: "system", Content: "be nice"}, {Role: "user", Content: "hi"}}},
		{Model: "DE", Messages: []model.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}},
		{Model: "DE", Messages: []model.Message{{Role: "tool", Content: "x"}}},
		{Model: "", Messages: []model.Message{{Role: "user", Content: "hi"}}},
		{Model: "DE", Messages: []model.Message{{Role: "user", Content: []any{map[string]any{"type": "image_url"}}}}},
	}
	for _, request := range rejects {
		if _, _, err := ConvertRequest(request); err == nil {
			t.Fatalf("%+v must fail loudly", request)
		}
	}
}

func TestUsageSplitsPromptAndCompletion(t *testing.T) {
	for _, stream := range []bool{false, true} {
		a := Adaptor{promptText: "abc"}
		w := httptest.NewRecorder()
		c := adaptor.NewContext(httptest.NewRequest("POST", "/", nil), w)
		usage, err := a.DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"translations":[{"text":"hello","billed_characters":3}]}`))}, &meta.Meta{IsStream: stream, ActualModelName: "EN-US"})
		if err != nil || usage.PromptTokens != 3 || usage.CompletionTokens != 5 || usage.TotalTokens != 8 || !strings.Contains(w.Body.String(), "hello") {
			t.Fatalf("%+v %v %s", usage, err, w.Body.String())
		}
	}
}
