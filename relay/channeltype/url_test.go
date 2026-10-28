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
		So(BaseURL(OpenAICompatible), ShouldBeEmpty)
		So(BaseURL(Proxy), ShouldBeEmpty)
		So(BaseURL(Unknown), ShouldBeEmpty)
		So(BaseURL(Dummy), ShouldBeEmpty)
	})
}

func TestIsValid(t *testing.T) {
	Convey("valid kinds", t, func() {
		So(IsValid(OpenAI), ShouldBeTrue)
		So(IsValid(OpenAIResponses), ShouldBeTrue)
		So(IsValid(Unknown), ShouldBeFalse)
		So(IsValid(Dummy), ShouldBeFalse)
		// Vacant retired slots must never validate.
		So(IsValid(2), ShouldBeFalse)
		So(IsValid(36), ShouldBeFalse)
		So(IsValid(51), ShouldBeFalse)
		So(IsValid(-1), ShouldBeFalse)
	})
}
