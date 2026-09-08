package talosprovisioner

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// addReplacementCertSAN uses the endpoint, VIP credentials, PKI and survivor
// SANs prepared before removal. Only the address allocated by server creation
// is new; applying it needs no provider lookup or credential reload.
func (p *Provisioner) addReplacementCertSAN(newServer *hcloud.Server, role string) error {
	if role != RoleControlPlane || p.hetznerOpts == nil || !p.hetznerOpts.FloatingIPEnabled {
		return nil
	}

	address, err := hetznerNodeTalosAddress(newServer)
	if err != nil {
		return err
	}

	config := p.configForRole(role)
	if config == nil {
		return ErrNoConfigForRole
	}

	sans := append([]string{}, config.K8sAPIServerConfig().CertSANs()...)
	sans = append(sans, address)

	updated, err := p.talosConfigs.WithCertSANs(sans)
	if err != nil {
		return fmt.Errorf("add replacement address to prepared config: %w", err)
	}

	p.talosConfigs = updated

	return nil
}

func (p *Provisioner) prepareReplacementFloatingIP(
	ctx context.Context, hzProvider *hetzner.Provider, clusterName, role string,
) (int64, error) {
	if role != RoleControlPlane || p.hetznerOpts == nil || !p.hetznerOpts.FloatingIPEnabled {
		return 0, nil
	}

	floatingIP, err := p.prepareFloatingIPConfig(ctx, hzProvider, clusterName)
	if err != nil {
		return 0, err
	}

	if floatingIP.ID <= 0 {
		return 0, ErrFloatingIPMissingForControlPlaneConfig
	}

	return floatingIP.ID, nil
}
