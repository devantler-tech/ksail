package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	clustercmd "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
)

type stubRepair struct {
	name   string
	result repairer.Result
}

func (s *stubRepair) Name() string { return s.name }

func (s *stubRepair) Run(_ context.Context, _ io.Writer) repairer.Result {
	return s.result
}

func TestRepairCmd_RunsRegisteredRepairs(t *testing.T) {
	t.Parallel()

	reg := repairer.NewRegistry()
	reg.Register(&stubRepair{
		name:   "fake-ok",
		result: repairer.Result{Name: "fake-ok", Status: repairer.StatusOK, Detail: "all good"},
	})

	cmd := clustercmd.NewRepairCmd(nil, reg)
	cmd.SetContext(context.Background())

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("execute: %v\nout: %s", err, out.String())
	}

	if !strings.Contains(out.String(), "fake-ok") {
		t.Fatalf("expected fake-ok in output: %s", out.String())
	}
}

// errStubFailure is a sentinel used by stub repairs in failure-path tests.
var errStubFailure = errors.New("stub repair failed")

func TestRepairCmd_FailsOnUnrepairable(t *testing.T) {
	t.Parallel()

	reg := repairer.NewRegistry()
	reg.Register(&stubRepair{name: "broken", result: repairer.Result{
		Name:   "broken",
		Status: repairer.StatusUnrepairable,
		Detail: "cannot fix",
		Err:    errStubFailure,
	}})

	cmd := clustercmd.NewRepairCmd(nil, reg)
	cmd.SetContext(context.Background())

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected non-nil error, got nil; out: %s", out.String())
	}
}

func TestRepairCmd_NoRepairsRegistered(t *testing.T) {
	t.Parallel()

	reg := repairer.NewRegistry()

	cmd := clustercmd.NewRepairCmd(nil, reg)
	cmd.SetContext(context.Background())

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}

	if !strings.Contains(out.String(), "no repairs registered") {
		t.Fatalf("expected 'no repairs registered' in output: %s", out.String())
	}
}
