package helpers_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	dockerpkg "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithDockerClientInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupMock      func(*dockerpkg.MockAPIClient)
		operation      func() error
		wantOperErr    bool
		wantCloseErr   bool
		wantOutputHas  string
		expectSnapshot bool
	}{
		{
			name: "successful operation and close",
			setupMock: func(m *dockerpkg.MockAPIClient) {
				m.EXPECT().Close().Return(nil)
			},
			operation: func() error {
				return nil
			},
		},
		{
			name: "operation error",
			setupMock: func(m *dockerpkg.MockAPIClient) {
				m.EXPECT().Close().Return(nil)
			},
			operation: func() error {
				return errors.New("operation failed")
			},
			wantOperErr: true,
		},
		{
			name: "close error logs warning",
			setupMock: func(m *dockerpkg.MockAPIClient) {
				m.EXPECT().Close().Return(errors.New("close failed"))
			},
			operation: func() error {
				return nil
			},
			wantCloseErr:   true,
			wantOutputHas:  "cleanup warning",
			expectSnapshot: true,
		},
		{
			name: "both operation and close error - operation error returned",
			setupMock: func(m *dockerpkg.MockAPIClient) {
				m.EXPECT().Close().Return(errors.New("close failed"))
			},
			operation: func() error {
				return errors.New("operation failed first")
			},
			wantOperErr:    true,
			wantCloseErr:   true,
			expectSnapshot: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockClient := dockerpkg.NewMockAPIClient(t)
			tt.setupMock(mockClient)

			var buf bytes.Buffer

			cmd := &cobra.Command{}
			cmd.SetOut(&buf)

			err := helpers.WithDockerClientInstance(
				cmd,
				mockClient,
				func(_ client.APIClient) error {
					return tt.operation()
				},
			)

			if tt.wantOperErr {
				require.Error(t, err)

				if tt.expectSnapshot {
					snaps.MatchSnapshot(t, "error: "+err.Error()+"\noutput: "+buf.String())
				}

				return
			}

			require.NoError(t, err)

			if tt.wantOutputHas != "" {
				assert.Contains(t, buf.String(), tt.wantOutputHas)
			}

			if tt.expectSnapshot {
				snaps.MatchSnapshot(t, buf.String())
			}
		})
	}
}

func TestWithDockerClientInstance_ClientPassedToOperation(t *testing.T) {
	t.Parallel()

	mockClient := dockerpkg.NewMockAPIClient(t)
	mockClient.EXPECT().Close().Return(nil)
	mockClient.EXPECT().ClientVersion().Return("1.42")

	var buf bytes.Buffer

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	var receivedVersion string

	err := helpers.WithDockerClientInstance(cmd, mockClient, func(c client.APIClient) error {
		receivedVersion = c.ClientVersion()

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "1.42", receivedVersion)
}
