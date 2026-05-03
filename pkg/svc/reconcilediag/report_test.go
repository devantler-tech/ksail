package reconcilediag_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	"github.com/stretchr/testify/assert"
)

func TestFailingResource_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resource reconcilediag.FailingResource
		want     string
	}{
		{
			name: "reason and message",
			resource: reconcilediag.FailingResource{
				Name:    "flux-system",
				Reason:  "ReconciliationFailed",
				Message: "validation error",
			},
			want: "flux-system: ReconciliationFailed — validation error",
		},
		{
			name: "with namespace",
			resource: reconcilediag.FailingResource{
				Name:      "cert-manager",
				Namespace: "cert-manager",
				Reason:    "InstallFailed",
				Message:   "timeout",
			},
			want: "cert-manager/cert-manager: InstallFailed — timeout",
		},
		{
			name: "reason only",
			resource: reconcilediag.FailingResource{
				Name:   "apps",
				Reason: "HealthCheckFailed",
			},
			want: "apps: HealthCheckFailed",
		},
		{
			name: "message only",
			resource: reconcilediag.FailingResource{
				Name:    "infra",
				Message: "dependency not ready",
			},
			want: "infra: dependency not ready",
		},
		{
			name: "no reason or message",
			resource: reconcilediag.FailingResource{
				Name: "unknown",
			},
			want: "unknown: not ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.resource.String())
		})
	}
}

func TestWarningEvent_String(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:     3 * time.Minute,
		Kind:    "Pod",
		Name:    "controller-abc",
		Message: "Back-off restarting (BackOff)",
	}

	assert.Equal(t, "3m ago: Pod/controller-abc — Back-off restarting (BackOff)", evt.String())
}

func TestWarningEvent_String_WithNamespace(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:       3 * time.Minute,
		Kind:      "Pod",
		Namespace: "flux-system",
		Name:      "controller-abc",
		Message:   "Back-off restarting (BackOff)",
	}

	assert.Equal(t, "3m ago: Pod/flux-system/controller-abc — Back-off restarting (BackOff)", evt.String())
}

func TestWarningEvent_String_Seconds(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:     45 * time.Second,
		Kind:    "HelmRelease",
		Name:    "nginx",
		Message: "install retries exhausted (Failed)",
	}

	assert.Equal(t, "45s ago: HelmRelease/nginx — install retries exhausted (Failed)", evt.String())
}

func TestWarningEvent_String_Hours(t *testing.T) {
	t.Parallel()

	evt := reconcilediag.WarningEvent{
		Age:     90 * time.Minute,
		Kind:    "Pod",
		Name:    "source-controller-xyz",
		Message: "OOMKilled (OOMKilled)",
	}

	assert.Equal(t, "1h30m ago: Pod/source-controller-xyz — OOMKilled (OOMKilled)", evt.String())
}

func TestReport_IsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report reconcilediag.Report
		want   bool
	}{
		{
			name:   "empty report",
			report: reconcilediag.Report{},
			want:   true,
		},
		{
			name: "empty sections no pods no events",
			report: reconcilediag.Report{
				Sections: []reconcilediag.ResourceSection{
					{Heading: "test", Resources: nil},
				},
			},
			want: true,
		},
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
			want: false,
		},
		{
			name: "with failing pods",
			report: reconcilediag.Report{
				FailingPods: "some pod failure",
			},
			want: false,
		},
		{
			name: "whitespace-only pods treated as empty",
			report: reconcilediag.Report{
				FailingPods: "   \n   ",
			},
			want: true,
		},
		{
			name: "with events",
			report: reconcilediag.Report{
				Events: []reconcilediag.WarningEvent{
					{Age: time.Minute, Kind: "Pod", Name: "x", Message: "err"},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.report.IsEmpty())
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
				Heading: "Failing Kustomizations",
				Resources: []reconcilediag.FailingResource{
					{
						Name:    "apps",
						Reason:  "HealthCheckFailed",
						Message: "Deployment/myapp not ready",
					},
				},
			},
			{
				Heading:   "Failing HelmReleases",
				Resources: nil,
			},
		},
		FailingPods:    "  controller-abc: CrashLoopBackOff for img:v1 (3 restarts)",
		EventNamespace: "flux-system",
		EventLookback:  5 * time.Minute,
		Events: []reconcilediag.WarningEvent{
			{Age: 2 * time.Minute, Kind: "Pod", Name: "ctrl", Message: "Back-off (BackOff)"},
		},
	}

	report.Write(&buf)

	output := buf.String()
	assert.Contains(t, output, "Reconciliation Diagnostics")
	assert.Contains(t, output, "Failing Kustomizations")
	assert.Contains(t, output, "apps: HealthCheckFailed")
	assert.NotContains(t, output, "Failing HelmReleases")
	assert.Contains(t, output, "failing pods")
	assert.Contains(t, output, "CrashLoopBackOff")
	assert.Contains(t, output, "warning events")
	assert.Contains(t, output, "Back-off")
}
