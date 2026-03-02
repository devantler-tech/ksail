package metallbinstaller

import (
	"context"
	"time"

	"k8s.io/client-go/dynamic"
)

// Exported for testing only - allows black-box tests to call unexported methods.

// TestWaitForCRDs exposes waitForCRDs for testing.
func (m *Installer) TestWaitForCRDs(ctx context.Context, client dynamic.Interface) error {
	return m.waitForCRDs(ctx, client)
}

// TestEnsureIPAddressPool exposes ensureIPAddressPool for testing.
func (m *Installer) TestEnsureIPAddressPool(ctx context.Context, client dynamic.Interface) error {
	return m.ensureIPAddressPool(ctx, client)
}

// TestEnsureL2Advertisement exposes ensureL2Advertisement for testing.
func (m *Installer) TestEnsureL2Advertisement(ctx context.Context, client dynamic.Interface) error {
	return m.ensureL2Advertisement(ctx, client)
}

// TestCRDPollInterval exposes crdPollInterval for testing.
const TestCRDPollInterval = 1 * time.Millisecond

// TestCRDPollTimeout exposes crdPollTimeout for testing.
const TestCRDPollTimeout = 100 * time.Millisecond
