package talosprovisioner_test

import (
	"errors"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errUnavailableMessage = errors.New(
		`rpc error: code = Unavailable desc = connection error: desc = ` +
			`"transport: authentication handshake failed: context deadline exceeded"`,
	)
	errHandshakeFailed = errors.New(
		"transport: authentication handshake failed: context deadline exceeded",
	)
	errPermissionDenied = errors.New("permission denied")
)

func TestIsRetryableTalosApplyConfigError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "NilError",
			err:  nil,
			want: false,
		},
		{
			name: "GRPCUnavailable",
			err:  status.Error(codes.Unavailable, "connection error"),
			want: true,
		},
		{
			name: "UnavailableMessage",
			err:  errUnavailableMessage,
			want: true,
		},
		{
			name: "HandshakeFailedMessage",
			err:  errHandshakeFailed,
			want: true,
		},
		{
			name: "NonRetryableGrpcError",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
			want: false,
		},
		{
			name: "NonRetryableError",
			err:  errPermissionDenied,
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.IsRetryableTalosApplyConfigError(testCase.err)
			if got != testCase.want {
				t.Fatalf("IsRetryableTalosApplyConfigError() = %v, want %v", got, testCase.want)
			}
		})
	}
}
