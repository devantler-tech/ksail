package awssso

// Waiters reports how many callers currently await the browser sign-in for the target's session.
func (manager *Manager) Waiters(target *Target) int {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	current, found := manager.flights[target.key]
	if !found {
		return 0
	}

	return current.waiters
}
