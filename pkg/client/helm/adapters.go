package helm

import (
	"time"

	helmv4action "helm.sh/helm/v4/pkg/action"
	helmv4kube "helm.sh/helm/v4/pkg/kube"
)

// actionConfig defines common configuration fields shared by Install and Upgrade actions.
type actionConfig interface {
	setWaitStrategy(strategy helmv4kube.WaitStrategy)
	setWaitForJobs(wait bool)
	setTimeout(timeout time.Duration)
	setVersion(version string)
}

// installActionAdapter wraps Install to implement actionConfig.
type installActionAdapter struct{ *helmv4action.Install }

func (a installActionAdapter) setWaitStrategy(s helmv4kube.WaitStrategy) { a.WaitStrategy = s }
func (a installActionAdapter) setWaitForJobs(w bool)                     { a.WaitForJobs = w }
func (a installActionAdapter) setTimeout(t time.Duration)                { a.Timeout = t }
func (a installActionAdapter) setVersion(v string)                       { a.Version = v }

// upgradeActionAdapter wraps Upgrade to implement actionConfig.
type upgradeActionAdapter struct{ *helmv4action.Upgrade }

func (a upgradeActionAdapter) setWaitStrategy(s helmv4kube.WaitStrategy) { a.WaitStrategy = s }
func (a upgradeActionAdapter) setWaitForJobs(w bool)                     { a.WaitForJobs = w }
func (a upgradeActionAdapter) setTimeout(t time.Duration)                { a.Timeout = t }
func (a upgradeActionAdapter) setVersion(v string)                       { a.Version = v }

// applyCommonActionConfig applies shared configuration from spec to action.
//
// When spec.Wait is true, this function configures the action to use
// StatusWatcherStrategy, which leverages kstatus (HIP-0022) for enhanced
// resource waiting. kstatus provides:
//   - Support for custom resources (via the ready condition)
//   - Full reconciliation monitoring (including cleanup of old pods)
//   - Consistent status checking across all resource types
//
// See: https://helm.sh/community/hips/hip-0022/
func applyCommonActionConfig(action actionConfig, spec *ChartSpec) {
	if spec.Wait {
		action.setWaitStrategy(helmv4kube.StatusWatcherStrategy)
	} else {
		action.setWaitStrategy(helmv4kube.HookOnlyStrategy)
	}

	action.setWaitForJobs(spec.WaitForJobs)

	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	action.setTimeout(timeout)
	action.setVersion(spec.Version)
}
