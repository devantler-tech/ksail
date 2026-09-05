package ephemeral_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type recordingClient struct {
	events *[]string
	failAt string
}

func (c recordingClient) Apply(ctx context.Context, obj *unstructured.Unstructured) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("request cancelled: %w", err)
	}

	name := obj.GetKind() + "/" + obj.GetName()

	*c.events = append(*c.events, name)
	if name == c.failAt {
		return assert.AnError
	}

	return nil
}

func (c recordingClient) WaitForCRD(ctx context.Context, name string) error {
	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("request cancelled: %w", err)
	}

	*c.events = append(*c.events, "established/"+name)
	if "established/"+name == c.failAt {
		return assert.AnError
	}

	return nil
}

func plannedResource(kind, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": kind, "metadata": map[string]any{"name": name},
	}}
}

func TestRunOrdersAdmissionPrerequisitesAndStopsOnFailure(t *testing.T) {
	t.Parallel()

	order := []string{
		"Namespace/test",
		"CustomResourceDefinition/widgets.example.com",
		"established/widgets.example.com",
		"ConfigMap/settings",
		"chart/operator",
		"Namespace/test",
		"CustomResourceDefinition/widgets.example.com",
		"ConfigMap/settings",
		"Widget/example",
	}
	for _, failAt := range append([]string{""}, order...) {
		t.Run("failure="+failAt, func(t *testing.T) {
			t.Parallel()

			plan := &ephemeral.Plan{
				Namespaces: []*unstructured.Unstructured{plannedResource("Namespace", "test")},
				CRDs: []*unstructured.Unstructured{
					plannedResource("CustomResourceDefinition", "widgets.example.com"),
				},
				Configuration: []*unstructured.Unstructured{
					plannedResource("ConfigMap", "settings"),
				},
				Charts:    []*helm.ChartSpec{{ReleaseName: "operator"}},
				Resources: []*unstructured.Unstructured{plannedResource("Widget", "example")},
			}

			var events []string

			client := recordingClient{events: &events, failAt: failAt}

			err := plan.Run(
				t.Context(),
				client,
				func(_ context.Context, chart *helm.ChartSpec) error {
					events = append(events, "chart/"+chart.ReleaseName)

					if failAt == "chart/operator" {
						return assert.AnError
					}

					return nil
				},
			)
			if failAt == "" {
				require.NoError(t, err)
				assert.Equal(t, order, events)

				return
			}

			require.ErrorIs(t, err, assert.AnError)
			assert.Equal(t, failAt, events[len(events)-1])
		})
	}
}

func TestRunStopsBeforeRequestsWhenCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	plan := &ephemeral.Plan{Charts: []*helm.ChartSpec{{ReleaseName: "operator"}}}
	err := plan.Run(ctx, nil, func(context.Context, *helm.ChartSpec) error {
		t.Error("chart install after cancellation")

		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestRunRechecksConfigurationAfterPolicyInstallation(t *testing.T) {
	t.Parallel()

	plan := &ephemeral.Plan{
		Configuration: []*unstructured.Unstructured{plannedResource("ConfigMap", "settings")},
		Charts:        []*helm.ChartSpec{{ReleaseName: "policy"}},
		Resources:     []*unstructured.Unstructured{plannedResource("Widget", "example")},
	}

	var events []string

	client := &recordingClient{events: &events}
	err := plan.Run(t.Context(), client, func(context.Context, *helm.ChartSpec) error {
		events = append(events, "chart/policy")
		client.failAt = "ConfigMap/settings"

		return nil
	})
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, []string{"ConfigMap/settings", "chart/policy", "ConfigMap/settings"}, events)
}
