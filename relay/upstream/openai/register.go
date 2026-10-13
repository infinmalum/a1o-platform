package openai

import "github.com/infinmalum/one-gateway/relay/upstream"

// register wires this package's credential refresher and capability validator
// into the profile registry. Importing the package is enough; the gateway
// wires the transport through the registry so no provider code leaks into the
// lifecycle.
func init() {
	if err := upstream.RegisterRefresher(ProfileID, Refresher{}); err != nil {
		panic("openai upstream: register refresher: " + err.Error())
	}
	if err := upstream.RegisterRequestValidator(ProfileID, ValidateResponsesRequest); err != nil {
		panic("openai upstream: register validator: " + err.Error())
	}
}
