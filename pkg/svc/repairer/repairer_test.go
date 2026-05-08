package repairer_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
)

// errBoom is a sentinel used by fakeRepair to test failure
// propagation; declared at package scope to satisfy err113.
var errBoom = errors.New("boom")

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

func TestRegistry_RegisterAndAll(t *testing.T) {
	t.Parallel()

	reg := repairer.NewRegistry()

	a := &fakeRepair{name: "a"}
	b := &fakeRepair{name: "b"}

	reg.Register(a)
	reg.Register(b)

	got := reg.All()
	if len(got) != 2 {
		t.Fatalf("expected 2 repairs, got %d", len(got))
	}

	if got[0].Name() != "a" || got[1].Name() != "b" {
		t.Fatalf("registration order not preserved: %s, %s", got[0].Name(), got[1].Name())
	}
}

func TestRunResultPropagates(t *testing.T) {
	t.Parallel()

	r := &fakeRepair{
		name:   "r",
		result: repairer.Result{Status: repairer.StatusUnrepairable, Err: errBoom},
	}

	got := r.Run(context.Background(), io.Discard)
	if !errors.Is(got.Err, errBoom) {
		t.Fatalf("expected errBoom, got %v", got.Err)
	}
}

func TestStatusString(t *testing.T) {
	t.Parallel()

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
