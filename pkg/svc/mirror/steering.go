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

// Guard rule placement: the agent's listener binds all interfaces (REDIRECT
// delivers remote traffic to the pod IP, not loopback — #6039), so a filter
// INPUT rule re-establishes least exposure by dropping every connection to the
// intercept port whose conntrack original destination was not this redirect's
// service port — e.g. a direct hit or an unrelated DNAT path.
const (
	guardTable = "filter"
	guardChain = "INPUT"
)

// SteeringRuleComment tags every rule the steering agent writes, so cleanup
// (and the idle-watchdog increment) can identify ksail's rules — and only
// ksail's — inside a chain that may carry unrelated entries.
const SteeringRuleComment = "ksail-steer"

// protocolTCP is the TCP protocol name as every mirror surface spells it: the
// iptables `-p` argument, the tcpdump BPF filter keyword, and the net.Dial
// network.
const protocolTCP = "tcp"

// Valid TCP port bounds for a steering redirect.
const (
	steeringPortMin = 1
	steeringPortMax = 65535
)

// ErrSteeringPortInvalid is returned when a redirect names a port outside the
// valid TCP range or uses the same port for the workload and agent listener.
var ErrSteeringPortInvalid = errors.New("steering port configuration invalid")

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

	if r.ServicePort == r.InterceptPort {
		return fmt.Errorf(
			"%w: service and intercept ports must differ (both are %d)",
			ErrSteeringPortInvalid,
			r.ServicePort,
		)
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

// GuardInsertArgs returns the iptables argument vector that installs the
// intercept-port guard at the head of the filter INPUT chain. Only connections
// whose conntrack original destination was this redirect's service port may
// reach the all-interfaces listener; direct hits and unrelated DNAT paths are
// dropped.
func (r SteeringRedirect) GuardInsertArgs() ([]string, error) {
	return r.guardArgs("-I")
}

// GuardDeleteArgs returns the iptables argument vector that removes the
// intercept-port guard — the exact inverse of [SteeringRedirect.GuardInsertArgs],
// for the same reversibility requirement as the redirect rule (#5839).
func (r SteeringRedirect) GuardDeleteArgs() ([]string, error) {
	return r.guardArgs("-D")
}

// args builds the full iptables argument vector for the given chain action,
// validating the redirect first so no malformed rule ever reaches a chain.
func (r SteeringRedirect) args(action string) ([]string, error) {
	err := r.Validate()
	if err != nil {
		return nil, err
	}

	return assembleRuleArgs(steeringTable, steeringChain, action, r.ruleSpec()), nil
}

// guardArgs builds the full iptables argument vector for the guard rule's
// chain action, with the same validate-first discipline as [SteeringRedirect.args].
func (r SteeringRedirect) guardArgs(action string) ([]string, error) {
	err := r.Validate()
	if err != nil {
		return nil, err
	}

	return assembleRuleArgs(guardTable, guardChain, action, r.guardRuleSpec()), nil
}

// assembleRuleArgs concatenates a rule's table/chain prefix and its
// specification into one iptables argument vector.
func assembleRuleArgs(table, chain, action string, spec []string) []string {
	prefix := []string{"-t", table, action, chain}

	args := make([]string, 0, len(prefix)+len(spec))
	args = append(args, prefix...)
	args = append(args, spec...)

	return args
}

// ruleSpec is the rule specification shared verbatim by insertion and
// deletion — iptables `-D` only matches a rule whose specification is
// byte-identical to the one `-I` installed.
func (r SteeringRedirect) ruleSpec() []string {
	return []string{
		"-p", protocolTCP,
		"--dport", strconv.Itoa(r.ServicePort),
		"-m", "comment", "--comment", SteeringRuleComment,
		"-j", "REDIRECT",
		"--to-ports", strconv.Itoa(r.InterceptPort),
	}
}

// guardRuleSpec is the guard rule specification shared verbatim by insertion
// and deletion, for the same byte-identical `-D` matching as [SteeringRedirect.ruleSpec].
func (r SteeringRedirect) guardRuleSpec() []string {
	return []string{
		"-p", protocolTCP,
		"--dport", strconv.Itoa(r.InterceptPort),
		"-m", "conntrack", "!", "--ctorigdstport", strconv.Itoa(r.ServicePort),
		"-m", "comment", "--comment", SteeringRuleComment,
		"-j", "DROP",
	}
}
