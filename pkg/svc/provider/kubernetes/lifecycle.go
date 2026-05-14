package kubernetes

import (
	"context"
	"fmt"

	"k8s.io/client-go/dynamic"
)

// SetupDinD is a composite operation that creates the namespace, DinD pod,
// and waits for readiness. It combines EnsureNamespace + CreateDinDPod + WaitForDinD.
func (p *Provider) SetupDinD(ctx context.Context, clusterName, distribution string) error {
	if err := p.EnsureNamespace(ctx, clusterName); err != nil {
		return fmt.Errorf("ensure namespace: %w", err)
	}

	if err := p.CreateDinDPod(ctx, clusterName, distribution); err != nil {
		return fmt.Errorf("create DinD pod: %w", err)
	}

	if err := p.WaitForDinD(ctx, clusterName); err != nil {
		return fmt.Errorf("wait for DinD: %w", err)
	}

	return nil
}

// TeardownDinD is a composite operation that removes API exposure, DinD pod,
// and the namespace. It combines DeleteAPIExposure + DeleteDinD + DeleteNodes.
func (p *Provider) TeardownDinD(ctx context.Context, dynClient dynamic.Interface, clusterName string) error {
	if err := p.DeleteAPIExposure(ctx, dynClient, clusterName); err != nil {
		return fmt.Errorf("delete API exposure: %w", err)
	}

	if err := p.DeleteDinD(ctx, clusterName); err != nil {
		return fmt.Errorf("delete DinD: %w", err)
	}

	if err := p.DeleteNodes(ctx, clusterName); err != nil {
		return fmt.Errorf("delete namespace: %w", err)
	}

	return nil
}
