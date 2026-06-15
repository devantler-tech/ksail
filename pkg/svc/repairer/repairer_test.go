package repairer_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/repairer"
)

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
