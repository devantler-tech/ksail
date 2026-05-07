package repairer_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
)

type fakeRepair struct {
	name   string
	result repairer.Result
	calls  int
	mu     sync.Mutex
}

func (f *fakeRepair) Name() string { return f.name }

func (f *fakeRepair) Run(_ context.Context, _ io.Writer) repairer.Result {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	return f.result
}

func TestRegisterAndAll(t *testing.T) {
	t.Cleanup(repairer.Reset)
	repairer.Reset()

	a := &fakeRepair{name: "a"}
	b := &fakeRepair{name: "b"}

	repairer.Register(a)
	repairer.Register(b)

	got := repairer.All()
	if len(got) != 2 {
		t.Fatalf("expected 2 repairs, got %d", len(got))
	}

	if got[0].Name() != "a" || got[1].Name() != "b" {
		t.Fatalf("registration order not preserved: %s, %s", got[0].Name(), got[1].Name())
	}
}

func TestReset(t *testing.T) {
	repairer.Reset()
	repairer.Register(&fakeRepair{name: "x"})
	repairer.Reset()

	if len(repairer.All()) != 0 {
		t.Fatal("Reset did not clear registry")
	}
}

func TestRunResultPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	r := &fakeRepair{name: "r", result: repairer.Result{Status: repairer.StatusUnrepairable, Err: wantErr}}

	got := r.Run(context.Background(), io.Discard)
	if !errors.Is(got.Err, wantErr) {
		t.Fatalf("expected wantErr, got %v", got.Err)
	}
}

func TestStatusString(t *testing.T) {
	cases := map[repairer.Status]string{
		repairer.StatusOK:           "ok",
		repairer.StatusRepaired:     "repaired",
		repairer.StatusUnrepairable: "unrepairable",
		repairer.StatusSkipped:      "skipped",
		repairer.Status(99):         "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}
