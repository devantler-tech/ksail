package ephemeral

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Client applies resources and observes CRD registration in an isolated cluster.
type Client interface {
	Apply(ctx context.Context, resource *unstructured.Unstructured) error
	WaitForCRD(ctx context.Context, name string) error
}

// Run applies prerequisites, installs charts, then checks workload admission.
// The caller supplies a shared deadline and owns unconditional cluster teardown.
func (p *Plan) Run(
	ctx context.Context,
	client Client,
	install func(context.Context, *helm.ChartSpec) error,
) error {
	err := applyResources(ctx, client, p.Namespaces)
	if err != nil {
		return err
	}

	err = applyResources(ctx, client, p.CRDs)
	if err != nil {
		return err
	}

	for _, crd := range p.CRDs {
		err := client.WaitForCRD(ctx, crd.GetName())
		if err != nil {
			return fmt.Errorf("establish %s: %w", resourceIdentity(crd), err)
		}
	}

	err = applyResources(ctx, client, p.Configuration)
	if err != nil {
		return err
	}

	for _, chart := range p.Charts {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("ephemeral chart installation cancelled: %w", err)
		}

		err = install(ctx, chart)
		if err != nil {
			return fmt.Errorf("install chart %s/%s: %w", chart.Namespace, chart.ReleaseName, err)
		}
	}

	err = p.recheckPrerequisites(ctx, client)
	if err != nil {
		return err
	}

	return applyResources(ctx, client, p.Resources)
}

// Reapply bootstrap objects so chart-installed policies can validate their updates.
// Policies scoped only to CREATE cannot retroactively validate bootstrap creation.
func (p *Plan) recheckPrerequisites(ctx context.Context, client Client) error {
	if len(p.Charts) == 0 {
		return nil
	}

	for _, resources := range [][]*unstructured.Unstructured{p.Namespaces, p.CRDs, p.Configuration} {
		err := applyResources(ctx, client, resources)
		if err != nil {
			return fmt.Errorf("recheck prerequisites after chart installation: %w", err)
		}
	}

	return nil
}

func applyResources(
	ctx context.Context,
	client Client,
	resources []*unstructured.Unstructured,
) error {
	for _, obj := range resources {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("ephemeral admission cancelled: %w", err)
		}

		err = client.Apply(ctx, obj)
		if err != nil {
			return fmt.Errorf("apply %s: %w", resourceIdentity(obj), err)
		}
	}

	return nil
}
