package hetzner

import "sync"

// firewallRuleLocks holds one mutex per firewall name. Every read, build and write of a
// firewall's rules holds that firewall's mutex, so a rule list built from an older snapshot
// cannot replace a newer one. It is shared by every Provider in the process; separate
// KSail processes are not serialized.
var firewallRuleLocks sync.Map

// lockFirewallRules locks the rules of the firewall named firewallName and returns the
// function that unlocks them.
func lockFirewallRules(firewallName string) func() {
	value, _ := firewallRuleLocks.LoadOrStore(firewallName, &sync.Mutex{})

	mutex, _ := value.(*sync.Mutex)
	mutex.Lock()

	return mutex.Unlock
}
