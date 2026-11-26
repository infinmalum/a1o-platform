package anthropic

import "github.com/infinmalum/a1o-platform/relay/upstream"

// register wires this package's credential refresher and OAuth connector into
// the profile registry. Importing the package is enough; the gateway wires
// the transport through the registry so no provider code leaks into the
// lifecycle.
func init() {
	if err := upstream.RegisterRefresher(ProfileID, Refresher{}); err != nil {
		panic("anthropic upstream: register refresher: " + err.Error())
	}
	if err := upstream.RegisterConnector(ProfileID, Connect{}); err != nil {
		panic("anthropic upstream: register connector: " + err.Error())
	}
}
