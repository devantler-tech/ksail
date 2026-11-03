package workload_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/render"
	"github.com/stretchr/testify/require"
)

// TestValidationCacheIsolation keeps cache selection independent of explicit refresh.
// Cancellation makes any refresh preparation fail before it can contact a registry.
func TestValidationCacheIsolation(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"default", "isolated", "refresh"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			var opts *kubeconform.ValidationOptions

			switch mode {
			case "isolated":
				opts = &kubeconform.ValidationOptions{CacheDirectory: t.TempDir()}
			case "refresh":
				refresh, err := kubeconform.NewSchemaRefresh()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, refresh.Close()) })

				opts = refresh.ValidationOptions(nil)
			}

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			data := []byte(
				"apiVersion: cache-mode.ksail.io/v1\nkind: Widget\nmetadata:\n  name: sample\n",
			)

			t.Run("substitutions", func(t *testing.T) {
				t.Parallel()

				expanded, err := workload.ExportExpandValidationSubstitutions(ctx, data, opts)
				if mode == "refresh" {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.NoError(t, err, "cache selection must not start refresh preparation")
					require.Equal(t, data, expanded)
				}
			})

			t.Run("rendered", func(t *testing.T) {
				t.Parallel()

				result := render.Result{Documents: []render.Document{{
					Bytes:      data,
					Provenance: render.Provenance{Origin: render.OriginRendered},
				}}}

				finished, err := workload.ExportFinishRenderedValidationSubstitutions(
					ctx, result, opts,
				)
				if mode == "refresh" {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.NoError(t, err, "isolating the final cache must not force refresh")
					require.Equal(t, result.Bytes(), finished)
				}
			})
		})
	}
}
