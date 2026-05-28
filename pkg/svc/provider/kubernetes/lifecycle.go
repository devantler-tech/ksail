package kubernetes

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"k8s.io/client-go/dynamic"
)

// SetupDinD is a composite operation that creates the namespace, DinD pod,
// and waits for readiness. It combines EnsureNamespace + CreateDinDPod + WaitForDinD.
func (p *Provider) SetupDinD(
	ctx context.Context,
	clusterName, distribution string,
	persistence v1alpha1.KubernetesPersistence,
) error {
	err := p.EnsureNamespace(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}

	// The DinD pod references the namespace's default ServiceAccount, which the
	// ServiceAccount controller provisions asynchronously. Wait for it so pod creation
	// does not race ahead with "serviceaccount default not found" (notably right after a
	// cluster restart, when the controller is still reconciling new namespaces).
	err = p.WaitForDefaultServiceAccount(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("wait for default service account: %w", err)
	}

	err = p.CreateDinDPod(ctx, clusterName, distribution, persistence)
	if err != nil {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	err = p.WaitForDinD(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("wait for DinD: %w", err)
	}

	return nil
}

// TeardownDinD is a composite operation that removes API exposure, DinD pod,
// and the namespace. It combines DeleteAPIExposure + DeleteDinD + DeleteNodes.
func (p *Provider) TeardownDinD(
	ctx context.Context,
	dynClient dynamic.Interface,
	clusterName string,
) error {
	err := p.DeleteAPIExposure(ctx, dynClient, clusterName)
	if err != nil {
		return fmt.Errorf("delete API exposure: %w", err)
	}

	err = p.DeleteDinD(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("delete DinD: %w", err)
	}

	err = p.DeleteNodes(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("delete namespace: %w", err)
	}

	return nil
}
