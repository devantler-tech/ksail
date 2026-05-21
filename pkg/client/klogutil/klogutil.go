// Package klogutil centralises silencing of client-go's klog output.
//
// client-go emits internal retry/connection errors (e.g. discovery
// "Unhandled Error" lines from memcache) directly via klog. For a polished CLI
// these must never leak into KSail's own command output, so KSail silences klog
// once at process startup. The k9s TUI relies on the same behaviour because it
// embeds only the k9s cmd/ subpackage and therefore never runs k9s' own
// klog-init from main.go.
package klogutil

import (
	"flag"
	"sync"

	"k8s.io/klog/v2"
)

// silenceOnce ensures klog is configured at most once per process; klog flag
// registration on the default flag.CommandLine must not run more than once.
//
//nolint:gochecknoglobals // sync.Once must be package-scoped to deduplicate klog flag registration.
var silenceOnce sync.Once

// Silence redirects klog output away from stderr and raises its threshold so
// client-go log lines do not pollute KSail command output. It intentionally
// suppresses all client-go klog output process-wide; KSail surfaces its own
// errors. Safe to call multiple times — the configuration is applied at most
// once per process.
func Silence() {
	silenceOnce.Do(func() {
		// klog.InitFlags panics if its flags are already registered on the
		// default flag.CommandLine (e.g. by another dependency). Only bind
		// klog's flags if they aren't already present.
		if flag.Lookup("logtostderr") == nil {
			klog.InitFlags(nil)
		}

		for name, value := range map[string]string{
			"logtostderr":     "false",
			"alsologtostderr": "false",
			"stderrthreshold": "fatal",
			"v":               "-10",
		} {
			// Errors here would only occur if klog's flag names change upstream.
			_ = flag.Set(name, value)
		}
	})
}
