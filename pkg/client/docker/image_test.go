//nolint:err113 // Tests use dynamic errors for mock behaviors
package docker_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestPullImage(t *testing.T) {
	t.Parallel()

	t.Run("pulls image successfully", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			ImagePull(ctx, "nginx:latest", mock.Anything).
			Return(io.NopCloser(strings.NewReader("pulling...")), nil).
			Once()

		err := docker.PullImage(ctx, mockClient, "nginx:latest")

		require.NoError(t, err)
	})

	t.Run("returns error when image pull request fails", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			ImagePull(ctx, "invalid:image", mock.Anything).
			Return(nil, errors.New("unauthorized")).
			Once()

		err := docker.PullImage(ctx, mockClient, "invalid:image")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image pull request")
	})

	t.Run("returns error when reading pull output fails", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		failReader := &failingReader{err: errors.New("read error")}

		mockClient.EXPECT().
			ImagePull(ctx, "registry:3", mock.Anything).
			Return(io.NopCloser(failReader), nil).
			Once()

		err := docker.PullImage(ctx, mockClient, "registry:3")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading image pull output")
	})

	t.Run("returns error when closing reader fails", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		// Use a reader that reads OK but fails on Close
		failCloser := &failingCloser{
			Reader: strings.NewReader("output"),
			err:    errors.New("close error"),
		}

		mockClient.EXPECT().
			ImagePull(ctx, "registry:3", mock.Anything).
			Return(failCloser, nil).
			Once()

		err := docker.PullImage(ctx, mockClient, "registry:3")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "closing image pull reader")
	})

	t.Run("pulls with empty output successfully", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			ImagePull(ctx, "alpine:latest", mock.Anything).
			Return(io.NopCloser(strings.NewReader("")), nil).
			Once()

		err := docker.PullImage(ctx, mockClient, "alpine:latest")

		require.NoError(t, err)
	})
}

// failingReader implements io.Reader that always returns an error.
type failingReader struct {
	err error
}

func (r *failingReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

// failingCloser implements io.ReadCloser with a reader that succeeds but Close fails.
type failingCloser struct {
	io.Reader

	err error
}

func (c *failingCloser) Close() error {
	return c.err
}
