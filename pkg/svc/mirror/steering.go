package mirror

import (
	"errors"
	"fmt"
	"strconv"
)

// Steering rule placement: intercept steers inbound TCP in the pod's OWN
// network namespace, so the rules live in the nat table's PREROUTING chain —
// the earliest hook inbound packets traverse, and the only place a REDIRECT
// can retarget them before the app container's socket accepts them.
const (
	steeringTable = "nat"
	steeringChain = "PREROUTING"
)

// SteeringRuleComment tags every rule the steering agent writes, so cleanup
// (and the idle-watchdog increment) can identify ksail's rules — and only
// ksail's — inside a chain that may carry unrelated entries.
const SteeringRuleComment = "ksail-steer"

// Valid TCP port bounds for a steering redirect.
const (
	steeringPortMin = 1
	steeringPortMax = 65535
)

// ErrSteeringPortInvalid is returned when a redirect names a port outside the
// valid TCP range.
var ErrSteeringPortInvalid = errors.New("steering port out of range")

// SteeringRedirect describes one traffic-steering rule: inbound TCP to the
// workload's ServicePort is redirected — same network namespace, via iptables
// NAT REDIRECT — to the InterceptPort the steering agent listens on. The
// original destination stays recoverable via SO_ORIGINAL_DST. TPROXY is
// deliberately not used: it needs policy routing and more privileges for no
// benefit inside a single pod netns (#5839).
type SteeringRedirect struct {
	// ServicePort is the workload port whose inbound TCP is steered.
	ServicePort int
	// InterceptPort is the steering agent's listener the traffic is
	// redirected to.
	InterceptPort int
}

// Validate reports whether both ports are inside the valid TCP range.
func (r SteeringRedirect) Validate() error {
	ports := []struct {
		name  string
		value int
	}{
		{"service", r.ServicePort},
		{"intercept", r.InterceptPort},
	}
	for _, port := range ports {
		if port.value < steeringPortMin || port.value > steeringPortMax {
			return fmt.Errorf("%w: %s port %d", ErrSteeringPortInvalid, port.name, port.value)
		}
	}

	return nil
}

// InsertArgs returns the iptables argument vector that installs the redirect
// at the head of the nat PREROUTING chain (`-I`, so it wins over any later
// rules for the same port).
func (r SteeringRedirect) InsertArgs() ([]string, error) {
	return r.args("-I")
}

// DeleteArgs returns the iptables argument vector that removes the redirect.
// It is the exact inverse of [SteeringRedirect.InsertArgs] (`-D` with an
// otherwise identical rule specification), which is what makes cleanup
// verifiable: reversibility is a hard requirement because ephemeral containers
// cannot be removed — only the rules can be (#5839).
func (r SteeringRedirect) DeleteArgs() ([]string, error) {
	return r.args("-D")
}

// args builds the full iptables argument vector for the given chain action,
// validating the redirect first so no malformed rule ever reaches a chain.
func (r SteeringRedirect) args(action string) ([]string, error) {
	err := r.Validate()
	if err != nil {
		return nil, err
	}

	prefix := []string{"-t", steeringTable, action, steeringChain}
	spec := r.ruleSpec()

	args := make([]string, 0, len(prefix)+len(spec))
	args = append(args, prefix...)
	args = append(args, spec...)

	return args, nil
}

// ruleSpec is the rule specification shared verbatim by insertion and
// deletion — iptables `-D` only matches a rule whose specification is
// byte-identical to the one `-I` installed.
func (r SteeringRedirect) ruleSpec() []string {
	return []string{
		"-p", "tcp",
		"--dport", strconv.Itoa(r.ServicePort),
		"-m", "comment", "--comment", SteeringRuleComment,
		"-j", "REDIRECT",
		"--to-ports", strconv.Itoa(r.InterceptPort),
	}
}
