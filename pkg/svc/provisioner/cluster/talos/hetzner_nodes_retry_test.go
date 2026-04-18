package talosprovisioner

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
			err: errors.New(
				`rpc error: code = Unavailable desc = connection error: desc = "transport: authentication handshake failed: context deadline exceeded"`,
			),
			want: true,
		},
		{
			name: "HandshakeFailedMessage",
			err:  errors.New("transport: authentication handshake failed: context deadline exceeded"),
			want: true,
		},
		{
			name: "NonRetryableGrpcError",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
			want: false,
		},
		{
			name: "NonRetryableError",
			err:  errors.New("permission denied"),
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := isRetryableTalosApplyConfigError(testCase.err)
			if got != testCase.want {
				t.Fatalf("isRetryableTalosApplyConfigError() = %v, want %v", got, testCase.want)
			}
		})
	}
}
