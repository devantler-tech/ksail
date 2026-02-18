package hcloudccminstaller

import (
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := helm.NewMockInterface(t)
			installer := NewInstaller(mockClient, tt.kubeconfig, tt.context, tt.timeout)

			if tt.wantNil {
				assert.Nil(t, installer, tt.description)
			} else {
				require.NotNil(t, installer, tt.description)
				assert.NotNil(t, installer.Base, "Base should be initialized")
				assert.Equal(t, tt.kubeconfig, installer.kubeconfig, "kubeconfig should match")
				assert.Equal(t, tt.context, installer.context, "context should match")
			}
		})
	}
}

func TestErrHetznerTokenNotSet(t *testing.T) {
	t.Parallel()

	assert.Error(t, ErrHetznerTokenNotSet)
	assert.Contains(t, ErrHetznerTokenNotSet.Error(), "HCLOUD_TOKEN")
	assert.Contains(t, ErrHetznerTokenNotSet.Error(), "not set")
}
