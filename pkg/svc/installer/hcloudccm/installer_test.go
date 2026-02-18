package hcloudccminstaller_test

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	hcloudccminstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/hcloudccm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kubeconfig  string
		context     string
		timeout     time.Duration
		wantNil     bool
		description string
	}{
		{
			name:        "creates installer with valid parameters",
			kubeconfig:  "/path/to/kubeconfig",
			context:     "test-context",
			timeout:     5 * time.Minute,
			wantNil:     false,
			description: "Should successfully create an installer instance",
		},
		{
			name:        "creates installer with empty kubeconfig",
			kubeconfig:  "",
			context:     "test-context",
			timeout:     5 * time.Minute,
			wantNil:     false,
			description: "Empty kubeconfig should still create installer",
		},
		{
			name:        "creates installer with zero timeout",
			kubeconfig:  "/path/to/kubeconfig",
			context:     "test-context",
			timeout:     0,
			wantNil:     false,
			description: "Zero timeout should still create installer",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			installer := hcloudccminstaller.NewInstaller(
				mockClient, testCase.kubeconfig, testCase.context, testCase.timeout,
			)

			if testCase.wantNil {
				assert.Nil(t, installer, testCase.description)
			} else {
				require.NotNil(t, installer, testCase.description)
			}
		})
	}
}

func TestErrHetznerTokenNotSet(t *testing.T) {
	t.Parallel()

	require.Error(t, hcloudccminstaller.ErrHetznerTokenNotSet)
	assert.Contains(t, hcloudccminstaller.ErrHetznerTokenNotSet.Error(), "HCLOUD_TOKEN")
	assert.Contains(t, hcloudccminstaller.ErrHetznerTokenNotSet.Error(), "not set")
}
