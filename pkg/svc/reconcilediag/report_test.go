package reconcilediag_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingResourceStringCase is one FailingResource.String scenario.
type failingResourceStringCase struct {
	name     string
	resource reconcilediag.FailingResource
	want     string
}

func runFailingResourceStringCases(t *testing.T, cases []failingResourceStringCase) {
	t.Helper()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.resource.String())
		})
	}
}

func TestFailingResource_String_WithDetail(t *testing.T) {
	t.Parallel()

	runFailingResourceStringCases(t, []failingResourceStringCase{
		{
			name: "reason and message",
			resource: reconcilediag.FailingResource{
				Name:    fluxSystemNS,
				Reason:  "ReconciliationFailed",
				Message: "validation error",
			},
			want: "flux-system: ReconciliationFailed · validation error",
		},
		{
			name: "with namespace",
			resource: reconcilediag.FailingResource{
				Name:      "cert-manager",
				Namespace: "cert-manager",
				Reason:    "InstallFailed",
				Message:   "timeout",
			},
			want: "cert-manager/cert-manager: InstallFailed · timeout",
		},
		{
			name: "blocked by dependency",
			resource: reconcilediag.FailingResource{
				Name:    appsName,
				Reason:  dependencyNotReady,
				Message: infraDepNotReadyMsg,
			},
			want: "apps: blocked by infrastructure",
		},
	})
}

func TestFailingResource_String_Fallbacks(t *testing.T) {
	t.Parallel()

	runFailingResourceStringCases(t, []failingResourceStringCase{
		{
			name:     "reason only",
			resource: reconcilediag.FailingResource{Name: appsName, Reason: healthCheckFailed},
			want:     "apps: HealthCheckFailed",
		},
		{
			name:     "message only",
			resource: reconcilediag.FailingResource{Name: "infra", Message: "dependency not ready"},
			want:     "infra: dependency not ready",
		},
		{
			name:     "no reason or message",
			resource: reconcilediag.FailingResource{Name: "unknown"},
			want:     "unknown: not ready",
		},
	})
}

func TestWarningEvent_String(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:     3 * time.Minute,
		Kind:    podKind,
		Name:    "controller-abc",
		Message: "Back-off restarting (BackOff)",
	}

	assert.Equal(t, "3m ago  Pod/controller-abc  Back-off restarting (BackOff)", evt.String())
}

func TestWarningEvent_String_OmitsNamespace(t *testing.T) {
	t.Parallel()

	// Events are grouped under a per-namespace heading, so the involved object's
	// namespace is intentionally omitted from each line.
	evt := reconcilediag.WarningEvent{
		Age:       3 * time.Minute,
		Kind:      podKind,
		Namespace: fluxSystemNS,
		Name:      "controller-abc",
		Message:   "Back-off restarting (BackOff)",
	}

	assert.Equal(
		t,
		"3m ago  Pod/controller-abc  Back-off restarting (BackOff)",
		evt.String(),
	)
}

func TestWarningEvent_String_Seconds(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:     45 * time.Second,
		Kind:    "HelmRelease",
		Name:    "nginx",
		Message: "install retries exhausted (Failed)",
	}

	assert.Equal(t, "45s ago  HelmRelease/nginx  install retries exhausted (Failed)", evt.String())
}

func TestWarningEvent_String_Hours(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:     90 * time.Minute,
		Kind:    podKind,
		Name:    "source-controller-xyz",
		Message: "OOMKilled (OOMKilled)",
	}

	assert.Equal(t, "1h30m ago  Pod/source-controller-xyz  OOMKilled (OOMKilled)", evt.String())
}

func TestWarningEvent_String_ShortensVerboseMessage(t *testing.T) {
	t.Parallel()

	// A real Flux health-check timeout message: sub-second precision is trimmed
	// and the bracketed resource list collapses to a count.
	evt := reconcilediag.WarningEvent{
		Age:  0,
		Kind: "Kustomization",
		Name: infraControllersName,
		Message: "health check failed after 25m0.04814184s: timeout waiting for: " +
			"[HelmRelease/opencost/opencost status: 'InProgress', " +
			"HelmRelease/monitoring/loki status: 'InProgress'] (HealthCheckFailed)",
	}

	got := evt.String()
	assert.Contains(t, got, "0s ago  Kustomization/infrastructure-controllers")
	assert.Contains(t, got, "health check failed after 25m")
	assert.Contains(t, got, "2 resources")
	assert.NotContains(t, got, "0.04814184")
	assert.NotContains(t, got, "InProgress")
}

func TestReport_IsEmpty_WhenTrue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report reconcilediag.Report
	}{
		{
			name:   "empty report",
			report: reconcilediag.Report{},
		},
		{
			name: "empty sections no pods no events",
			report: reconcilediag.Report{
				Sections: []reconcilediag.ResourceSection{
					{Heading: "test", Resources: nil},
				},
			},
		},
		{
			name: "whitespace-only pods treated as empty",
			report: reconcilediag.Report{
				FailingPods: "   \n   ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, tt.report.IsEmpty())
		})
	}
}

func TestReport_IsEmpty_WhenFalse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report reconcilediag.Report
	}{
		{
			name: "with failing resources",
			report: reconcilediag.Report{
				Sections: []reconcilediag.ResourceSection{
					{
						Heading: "test",
						Resources: []reconcilediag.FailingResource{
							{Name: "foo", Reason: "Failed"},
						},
					},
				},
			},
		},
		{
			name: "with failing pods",
			report: reconcilediag.Report{
				FailingPods: "some pod failure",
			},
		},
		{
			name: "with events",
			report: reconcilediag.Report{
				Events: []reconcilediag.WarningEvent{
					{Age: time.Minute, Kind: podKind, Name: "x", Message: "err"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, tt.report.IsEmpty())
		})
	}
}

func TestReport_Write_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	report := &reconcilediag.Report{}
	report.Write(&buf)

	assert.Empty(t, buf.String())
}

func TestReport_Write_FullReport(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	report := &reconcilediag.Report{
		Sections: []reconcilediag.ResourceSection{
			{
				Heading: kustomizationsHeading,
				Resources: []reconcilediag.FailingResource{
					{
						Name:    appsName,
						Reason:  healthCheckFailed,
						Message: "Deployment/myapp not ready",
					},
				},
			},
			{
				Heading:   helmReleasesHeading,
				Resources: nil,
			},
		},
		FailingPods:    "  controller-abc: CrashLoopBackOff for img:v1 (3 restarts)",
		EventNamespace: fluxSystemNS,
		EventLookback:  5 * time.Minute,
		Events: []reconcilediag.WarningEvent{
			{Age: 2 * time.Minute, Kind: podKind, Name: "ctrl", Message: "Back-off (BackOff)"},
		},
	}

	report.Write(&buf)

	output := buf.String()
	assert.Contains(t, output, "Reconciliation failed")
	assert.Contains(t, output, kustomizationsHeading)
	assert.Contains(t, output, "✗ apps")
	assert.Contains(t, output, "HealthCheckFailed · Deployment/myapp not ready")
	// HelmReleases section is empty, so its heading must not be printed.
	assert.NotContains(t, output, helmReleasesHeading)
	assert.Contains(t, output, "failing pods")
	assert.Contains(t, output, "CrashLoopBackOff")
	assert.Contains(t, output, "recent warnings")
	assert.Contains(t, output, "Back-off")
}

func TestReport_Write_OrdersRootsBeforeCascades(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// apps → infrastructure → infrastructure-controllers (the active root).
	// Despite the input being shuffled, the output must list the root first and
	// each blocked resource below whatever it is blocked by.
	report := &reconcilediag.Report{
		Sections: []reconcilediag.ResourceSection{{
			Heading: kustomizationsHeading,
			Resources: []reconcilediag.FailingResource{
				{
					Name:    appsName,
					Reason:  dependencyNotReady,
					Message: infraDepNotReadyMsg,
				},
				{
					Name:    "infrastructure",
					Reason:  dependencyNotReady,
					Message: "dependency 'flux-system/infrastructure-controllers' is not ready",
				},
				{Name: infraControllersName, Reason: "Progressing", Message: "in progress"},
			},
		}},
	}

	report.Write(&buf)
	output := buf.String()

	rootIdx := strings.Index(output, "► infrastructure-controllers")
	infraIdx := strings.Index(output, "· infrastructure ")
	appsIdx := strings.Index(output, "· apps")

	require.NotEqual(t, -1, rootIdx)
	require.NotEqual(t, -1, infraIdx)
	require.NotEqual(t, -1, appsIdx)
	assert.Less(t, rootIdx, infraIdx, "active root must come before its dependents")
	assert.Less(t, infraIdx, appsIdx, "a resource must appear below what it is blocked by")
}

// reportSummaryCase is one Report.Summary scenario.
type reportSummaryCase struct {
	name   string
	report reconcilediag.Report
	want   string
}

// kustSection builds a single-section report holding the given Kustomizations.
func kustSection(resources ...reconcilediag.FailingResource) reconcilediag.Report {
	return reconcilediag.Report{
		Sections: []reconcilediag.ResourceSection{
			{Heading: kustomizationsHeading, Resources: resources},
		},
	}
}

func runReportSummaryCases(t *testing.T, cases []reportSummaryCase) {
	t.Helper()

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.report.Summary())
		})
	}
}

func TestReport_Summary_NamesRoots(t *testing.T) {
	t.Parallel()

	runReportSummaryCases(t, []reportSummaryCase{
		{
			name:   "empty report",
			report: reconcilediag.Report{},
			want:   "",
		},
		{
			name: "single root with reason",
			report: kustSection(
				reconcilediag.FailingResource{
					Name:    appsName,
					Reason:  healthCheckFailed,
					Message: "timed out",
				},
			),
			want: "reconciliation failed: apps (HealthCheckFailed)",
		},
		{
			name: "multiple roots",
			report: reconcilediag.Report{
				Sections: []reconcilediag.ResourceSection{{
					Heading: helmReleasesHeading,
					Resources: []reconcilediag.FailingResource{
						{Name: "loki", Namespace: "monitoring", Reason: "UpgradeFailed"},
						{Name: "alloy", Namespace: "monitoring", Reason: "InstallFailed"},
					},
				}},
			},
			want: "reconciliation failed: 2 resources not ready (monitoring/alloy, monitoring/loki)",
		},
	})
}

func TestReport_Summary_CountsBlockedAndFallbacks(t *testing.T) {
	t.Parallel()

	runReportSummaryCases(t, []reportSummaryCase{
		{
			name: "root with blocked dependents",
			report: kustSection(
				reconcilediag.FailingResource{
					Name:    infraControllersName,
					Reason:  "Progressing",
					Message: "in progress",
				},
				reconcilediag.FailingResource{
					Name:    "infrastructure",
					Reason:  dependencyNotReady,
					Message: "dependency 'flux-system/infrastructure-controllers' is not ready",
				},
				reconcilediag.FailingResource{
					Name:    appsName,
					Reason:  dependencyNotReady,
					Message: infraDepNotReadyMsg,
				},
			),
			want: "reconciliation failed: infrastructure-controllers (Progressing); 2 dependents blocked",
		},
		{
			name: "only blocked",
			report: kustSection(
				reconcilediag.FailingResource{
					Name:    appsName,
					Reason:  dependencyNotReady,
					Message: "dependency 'x' is not ready",
				},
			),
			want: "reconciliation failed: 1 resources not ready",
		},
		{
			name:   "pods only, no resources",
			report: reconcilediag.Report{FailingPods: "controller-abc: CrashLoopBackOff"},
			want:   "reconciliation failed — see diagnostics above",
		},
	})
}

func TestReport_Summary_NilReceiver(t *testing.T) {
	t.Parallel()

	var report *reconcilediag.Report

	assert.Empty(t, report.Summary())
}
