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
// Exactly one of ImageID or ISOID must be set; both or neither is an error.
func (p *Provider) buildServerCreateOpts(opts CreateServerOpts) (hcloud.ServerCreateOpts, error) {
	if opts.ImageID > 0 && opts.ISOID > 0 {
		return hcloud.ServerCreateOpts{}, ErrImageAndISOBothSet
	}

	if opts.ImageID == 0 && opts.ISOID == 0 {
		return hcloud.ServerCreateOpts{}, ErrImageOrISORequired
	}

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
		// When using ISO, we need a placeholder image for the server disk.
		// The server must NOT start automatically so the ISO can be attached
		// before the first boot — otherwise the server boots from the Debian
		// disk image and never enters Talos maintenance mode.
		createOpts.Image = &hcloud.Image{
			Name: "debian-13",
		}
		createOpts.StartAfterCreate = new(false)
	} else {
		createOpts.Image = &hcloud.Image{
			ID: opts.ImageID,
		}
	}

	applyOptionalServerFields(opts, &createOpts)

	return createOpts, nil
}

// applyOptionalServerFields copies optional CreateServerOpts fields onto createOpts.
func applyOptionalServerFields(opts CreateServerOpts, createOpts *hcloud.ServerCreateOpts) {
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
}
