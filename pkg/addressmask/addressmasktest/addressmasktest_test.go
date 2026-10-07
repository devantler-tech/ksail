package addressmasktest_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/addressmask/addressmasktest"
)

// TestPublicAddressesIn pins the shapes the oracle must see: a check built on
// it is only worth something if an address in each of them is found.
func TestPublicAddressesIn(t *testing.T) {
	t.Parallel()

	found := map[string]int{
		"dial tcp 203.0.113.10:50000: connect: connection refused": 1,
		"endpoint (203.0.113.10) and [2001:db8::10]:6443.":         2,
		"https://203.0.113.10:6443; next":                          1,
		"from ::ffff:203.0.113.10":                                 1,
		"local 127.0.0.1:50000, 10.5.0.2, fe80::1, 03:44:37":       0,
		"<address hidden>:6443 on prod-control-plane-1":            0,
	}

	for text, want := range found {
		if got := addressmasktest.PublicAddressesIn(text); len(got) != want {
			t.Errorf("PublicAddressesIn(%q) = %v, want %d", text, got, want)
		}
	}
}
