package omni

import cosistate "github.com/cosi-project/runtime/pkg/state"

// NewProviderWithState creates a Provider with an injected COSI state for testing.
func NewProviderWithState(st cosistate.State) *Provider {
	return &Provider{st: st}
}
