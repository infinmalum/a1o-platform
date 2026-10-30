package channeltype

import (
	. "github.com/smartystreets/goconvey/convey"
	"testing"
)

func TestBaseURL(t *testing.T) {
	Convey("channel base urls", t, func() {
		So(BaseURL(OpenAI), ShouldEqual, "https://api.openai.com")
		So(BaseURL(OpenAIResponses), ShouldEqual, "https://api.openai.com")
		So(BaseURL(Anthropic), ShouldEqual, "https://api.anthropic.com")
		So(BaseURL(OpenRouter), ShouldEqual, "https://openrouter.ai/api/v1")
		// An unassigned ID resolves no base URL.
		So(BaseURL(43), ShouldBeEmpty)
	})
}

func TestIsValid(t *testing.T) {
	Convey("valid kinds", t, func() {
		So(IsValid(OpenAI), ShouldBeTrue)
		So(IsValid(OpenAIResponses), ShouldBeTrue)
		So(IsValid(OpenRouter), ShouldBeTrue)
		So(IsValid(Unknown), ShouldBeFalse)
		So(IsValid(Dummy), ShouldBeFalse)
		// An unassigned ID in the middle of the range is not a kind.
		So(IsValid(43), ShouldBeFalse)
		So(IsValid(-1), ShouldBeFalse)
	})
}
