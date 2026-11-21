package hetzner

import (
	"context"
	"fmt"
	"sync"
)

// firewallRuleLocks holds one lock per firewall name. Every read, build and write of a
// firewall's rules holds that firewall's lock, so a rule list built from an older snapshot
// cannot replace a newer one. It is shared by every Provider in the process; separate
// KSail processes are not serialized.
var firewallRuleLocks sync.Map

// lockFirewallRules locks the rules of the firewall named firewallName and returns the
// function that unlocks them. Waiting for the lock ends with ctx, so a bounded cleanup is
// not held past its deadline by another operation's long action wait.
func lockFirewallRules(ctx context.Context, firewallName string) (func(), error) {
	value, _ := firewallRuleLocks.LoadOrStore(firewallName, make(chan struct{}, 1))

	lock, _ := value.(chan struct{})

	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for firewall %s rules lock: %w", firewallName, ctx.Err())
	}
}
