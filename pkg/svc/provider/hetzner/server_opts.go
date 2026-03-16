package hetzner

import "github.com/hetznercloud/hcloud-go/v2/hcloud"

// CreateServerOpts contains options for creating a Hetzner server.
type CreateServerOpts struct {
	Name             string
	ServerType       string
	ImageID          int64 // Image ID (for snapshots) - mutually exclusive with ISOID
	ISOID            int64 // ISO ID (for Talos public ISOs) - mutually exclusive with ImageID
	Location         string
	Labels           map[string]string
	UserData         string
	NetworkID        int64
	PlacementGroupID int64
	SSHKeyID         int64
	FirewallIDs      []int64
}

// buildServerCreateOpts builds the hcloud.ServerCreateOpts from CreateServerOpts.
func (p *Provider) buildServerCreateOpts(opts CreateServerOpts) hcloud.ServerCreateOpts {
	createOpts := hcloud.ServerCreateOpts{
		Name:   opts.Name,
		Labels: opts.Labels,
		ServerType: &hcloud.ServerType{
			Name: opts.ServerType,
		},
		Location: &hcloud.Location{
			Name: opts.Location,
		},
		StartAfterCreate: new(true),
	}

	// Use either Image or ISO - ISOs are used for Talos public ISOs
	if opts.ISOID > 0 {
		// When using ISO, we need a placeholder image for the server disk
		// The ISO will be mounted and booted from
		createOpts.Image = &hcloud.Image{
			Name: "debian-13",
		}
		// Note: ISO attachment happens after server creation via AttachISO action
	} else if opts.ImageID > 0 {
		createOpts.Image = &hcloud.Image{
			ID: opts.ImageID,
		}
	}

	if opts.UserData != "" {
		createOpts.UserData = opts.UserData
	}

	if opts.NetworkID > 0 {
		createOpts.Networks = []*hcloud.Network{
			{ID: opts.NetworkID},
		}
	}

	if opts.PlacementGroupID > 0 {
		createOpts.PlacementGroup = &hcloud.PlacementGroup{
			ID: opts.PlacementGroupID,
		}
	}

	if opts.SSHKeyID > 0 {
		createOpts.SSHKeys = []*hcloud.SSHKey{
			{ID: opts.SSHKeyID},
		}
	}

	if len(opts.FirewallIDs) > 0 {
		firewalls := make([]*hcloud.ServerCreateFirewall, len(opts.FirewallIDs))

		for i, id := range opts.FirewallIDs {
			firewalls[i] = &hcloud.ServerCreateFirewall{
				Firewall: hcloud.Firewall{ID: id},
			}
		}

		createOpts.Firewalls = firewalls
	}

	return createOpts
}
