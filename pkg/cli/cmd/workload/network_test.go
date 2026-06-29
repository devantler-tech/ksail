package workload_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubObserver struct {
	records []hubble.FlowRecord
}

func (s stubObserver) ObserveFlows(_ context.Context, _ uint64) ([]hubble.FlowRecord, error) {
	return s.records, nil
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

	restore := workload.ExportSetFlowObserverFactory(func(_ string) hubble.FlowObserver {
		return stubObserver{records: []hubble.FlowRecord{
			{
				Verdict:     "FORWARDED",
				Protocol:    "TCP",
				Source:      hubble.Endpoint{Namespace: "default", Pod: "web"},
				Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "dns"},
			},
		}}
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
}
