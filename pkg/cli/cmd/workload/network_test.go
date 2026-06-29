package workload_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubObserver struct {
	records []hubble.FlowRecord
	// observed and streamed record which path ran, so a test can assert that
	// --follow selects StreamFlows and the one-shot path selects ObserveFlows
	// (both yield the same records, so output alone can't distinguish them).
	observed *bool
	streamed *bool
}

func (s stubObserver) ObserveFlows(_ context.Context, _ uint64) ([]hubble.FlowRecord, error) {
	if s.observed != nil {
		*s.observed = true
	}

	return s.records, nil
}

func (s stubObserver) StreamFlows(
	_ context.Context,
	_ uint64,
	emit func(hubble.FlowRecord) error,
) error {
	if s.streamed != nil {
		*s.streamed = true
	}

	for _, record := range s.records {
		err := emit(record)
		if err != nil {
			return err
		}
	}

	return nil
}

func TestNewNetworkCmdHasCorrectDefaults(t *testing.T) {
	t.Parallel()

	cmd := workload.NewNetworkCmd()

	assert.Equal(t, "network", cmd.Name())
	assert.Equal(t, "plain", cmd.Flags().Lookup("output").DefValue)
	assert.Equal(t, "localhost:4245", cmd.Flags().Lookup("server").DefValue)
	assert.Equal(t, "20", cmd.Flags().Lookup("last").DefValue)
	assert.NotNil(t, cmd.Flags().Lookup("namespace"))
	assert.NotNil(t, cmd.Flags().Lookup("pod"))
	assert.NotNil(t, cmd.Flags().Lookup("protocol"))
}

func TestNetworkCmdHelp(t *testing.T) {
	t.Parallel()

	cmd := workload.NewNetworkCmd()
	cmd.SetArgs([]string{"--help"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Hubble")
	assert.Contains(t, out.String(), "--protocol")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestNetworkCmdRequiresCilium(t *testing.T) {
	// Chdir to an empty directory so config discovery falls back to defaults
	// (CNI=Default), which must be rejected.
	t.Chdir(t.TempDir())

	cmd := workload.NewNetworkCmd()
	cmd.SetArgs([]string{})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.ErrorIs(t, err, workload.ErrCNINotCiliumExport)
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestNetworkCmdWithCiliumStreamsFlows(t *testing.T) {
	t.Chdir(t.TempDir())

	var observed, streamed bool

	restore := workload.ExportSetFlowObserverFactory(func(_ string) hubble.FlowObserver {
		return stubObserver{
			records: []hubble.FlowRecord{
				{
					Verdict:     "FORWARDED",
					Protocol:    "TCP",
					Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
					Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
				},
			},
			observed: &observed,
			streamed: &streamed,
		}
	})
	defer restore()

	cmd := workload.NewNetworkCmd()
	cmd.SetArgs([]string{"--cni", "Cilium", "--output", "json"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	output := out.String()
	assert.Contains(t, output, `"web"`)
	assert.Contains(t, output, "FORWARDED")
	assert.True(t, observed, "one-shot mode must call ObserveFlows")
	assert.False(t, streamed, "one-shot mode must not call StreamFlows")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestNetworkCmdFollowStreamsLiveFlows(t *testing.T) {
	t.Chdir(t.TempDir())

	var observed, streamed bool

	restore := workload.ExportSetFlowObserverFactory(func(_ string) hubble.FlowObserver {
		return stubObserver{
			records: []hubble.FlowRecord{
				{
					Verdict:     "FORWARDED",
					Protocol:    "TCP",
					Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
					Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
				},
			},
			observed: &observed,
			streamed: &streamed,
		}
	})
	defer restore()

	cmd := workload.NewNetworkCmd()
	cmd.SetArgs([]string{"--cni", "Cilium", "--follow"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	output := out.String()
	assert.Contains(t, output, "TIME", "follow plain output prints the streaming header")
	assert.Contains(t, output, "default/web")
	assert.Contains(t, output, "FORWARDED")
	assert.True(t, streamed, "follow mode must call StreamFlows")
	assert.False(t, observed, "follow mode must not call ObserveFlows")
}

func TestNetworkCmdHasFollowFlag(t *testing.T) {
	t.Parallel()

	cmd := workload.NewNetworkCmd()

	flag := cmd.Flags().Lookup("follow")
	require.NotNil(t, flag)
	assert.Equal(t, "f", flag.Shorthand)
	assert.Equal(t, "false", flag.DefValue)
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestNetworkCmdFollowWithJSONOutputWritesNDJSON(t *testing.T) {
	t.Chdir(t.TempDir())

	var observed, streamed bool

	restore := workload.ExportSetFlowObserverFactory(func(_ string) hubble.FlowObserver {
		return stubObserver{
			records: []hubble.FlowRecord{
				{
					Verdict:     "FORWARDED",
					Protocol:    "TCP",
					Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
					Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
				},
				{
					Verdict:     "DROPPED",
					Protocol:    "UDP",
					Source:      hubble.Endpoint{Namespace: "monitoring", Pod: "prom"},
					Destination: hubble.Endpoint{Namespace: "default", Pod: "app"},
				},
			},
			observed: &observed,
			streamed: &streamed,
		}
	})
	defer restore()

	cmd := workload.NewNetworkCmd()
	cmd.SetArgs([]string{"--cni", "Cilium", "--follow", "--output", "json"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	output := out.String()
	// Follow + JSON must produce NDJSON: no array wrapper.
	assert.NotContains(t, output, "[", "--follow --output json must not produce a JSON array")
	// Each line must be a standalone JSON object.
	lines := splitNonEmpty(output)
	require.Len(t, lines, 2, "one NDJSON line per flow")

	var first, second map[string]any

	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))

	assert.True(t, streamed, "follow+json mode must call StreamFlows")
	assert.False(t, observed, "follow+json mode must not call ObserveFlows")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestNetworkCmdFollowWithNamespaceFilter(t *testing.T) {
	t.Chdir(t.TempDir())

	restore := workload.ExportSetFlowObserverFactory(func(_ string) hubble.FlowObserver {
		return stubObserver{
			records: []hubble.FlowRecord{
				{
					Verdict:     "FORWARDED",
					Protocol:    "TCP",
					Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
					Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
				},
				{
					Verdict:     "DROPPED",
					Protocol:    "UDP",
					Source:      hubble.Endpoint{Namespace: "monitoring", Pod: "prom"},
					Destination: hubble.Endpoint{Namespace: "default", Pod: "app"},
				},
			},
		}
	})
	defer restore()

	cmd := workload.NewNetworkCmd()
	cmd.SetArgs([]string{"--cni", "Cilium", "--follow", "--namespace", "monitoring"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	output := out.String()
	// The "monitoring" namespace filter passes the second flow (source=monitoring/prom)
	// and the first flow has "monitoring" only as a destination namespace,
	// so both should be included (filter matches src or dst).
	assert.Contains(t, output, "monitoring/prom", "filter matches source namespace")
	assert.Contains(t, output, "TIME", "follow plain output always prints the header")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestNetworkCmdFollowShortFlagAlias(t *testing.T) {
	t.Chdir(t.TempDir())

	var streamed bool

	restore := workload.ExportSetFlowObserverFactory(func(_ string) hubble.FlowObserver {
		return stubObserver{
			records: []hubble.FlowRecord{
				{
					Verdict:  "FORWARDED",
					Protocol: "TCP",
					Source:   hubble.Endpoint{Namespace: "default", Pod: "web"},
				},
			},
			streamed: &streamed,
		}
	})
	defer restore()

	cmd := workload.NewNetworkCmd()
	// Use the short -f alias instead of the long --follow flag.
	cmd.SetArgs([]string{"--cni", "Cilium", "-f"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	assert.True(t, streamed, "-f shorthand must select follow/stream mode")
}

// splitNonEmpty splits s on newlines and returns only the non-blank lines.
func splitNonEmpty(s string) []string {
	var lines []string

	for _, line := range bytes.Split(bytes.TrimRight([]byte(s), "\n"), []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			lines = append(lines, string(line))
		}
	}

	return lines
}
