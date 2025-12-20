package ciliuminstaller_test

import (
	"testing"
	"time"

	ciliuminstaller "github.com/devantler-tech/ksail/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/require"
)

func TestNewCiliumInstaller(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewCiliumInstaller(
		nil,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
	)

	require.NotNil(t, installer, "expected installer to be created")
}

func TestNewCiliumInstaller_WithDifferentTimeout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "1 minute timeout",
			timeout: 1 * time.Minute,
		},
		{
			name:    "5 minute timeout",
			timeout: 5 * time.Minute,
		},
		{
			name:    "10 minute timeout",
			timeout: 10 * time.Minute,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			installer := ciliuminstaller.NewCiliumInstaller(
				nil,
				"/path/to/kubeconfig",
				"test-context",
				testCase.timeout,
			)

			require.NotNil(t, installer, "expected installer to be created")
		})
	}
}

func TestNewCiliumInstaller_WithEmptyParams(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewCiliumInstaller(
		nil,
		"",
		"",
		0,
	)

	require.NotNil(t, installer, "expected installer to be created even with empty params")
}
