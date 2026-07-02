package hetzner

import "github.com/hetznercloud/hcloud-go/v2/hcloud"

// CreateServerOpts contains options for creating a Hetzner server.
type CreateServerOpts struct {
	Name       string
	ServerType string
	// ImageName is a base OS image referenced by name (e.g. "ubuntu-24.04"), for
	// distributions that install onto a stock image via cloud-init (k3s, kubeadm).
	// The hcloud API resolves the name server-side, so no lookup call is needed.
	// Exactly one of ImageName, ImageID, or ISOID must be set.
	ImageName        string
	ImageID          int64 // Image ID (for snapshots) - exactly one of ImageName/ImageID/ISOID
	ISOID            int64 // ISO ID (for Talos public ISOs) - exactly one of ImageName/ImageID/ISOID
	Location         string
	Labels           map[string]string
	UserData         string
	NetworkID        int64
	PlacementGroupID int64
	SSHKeyID         int64
	FirewallIDs      []int64
	// EnableIPv4 controls whether the server is assigned a public IPv4 address.
	// nil means use the Hetzner default (a public IPv4 is assigned). Set to false
	// for IPv4-less nodes reached over the private network.
	EnableIPv4 *bool
	// EnableIPv6 controls whether the server is assigned a public IPv6 address.
	// nil means use the Hetzner default (a public IPv6 is assigned).
	EnableIPv6 *bool
}

// publicNetEnabled returns the pointed-to bool, defaulting to true when ptr is nil
// so an unset toggle preserves Hetzner's default of assigning a public IP.
func publicNetEnabled(ptr *bool) bool {
	return ptr == nil || *ptr
}

// imageSourceCount returns how many of the mutually exclusive boot-image sources
// (ImageName, ImageID, ISOID) are set on opts.
func imageSourceCount(opts CreateServerOpts) int {
	count := 0

	if opts.ImageName != "" {
		count++
	}

	if opts.ImageID > 0 {
		count++
	}

	if opts.ISOID > 0 {
		count++
	}

	return count
}

// buildServerCreateOpts builds the hcloud.ServerCreateOpts from CreateServerOpts.
// Exactly one of ImageName, ImageID, or ISOID must be set; more or none is an error.
func (p *Provider) buildServerCreateOpts(opts CreateServerOpts) (hcloud.ServerCreateOpts, error) {
	switch imageSourceCount(opts) {
	case 0:
		return hcloud.ServerCreateOpts{}, ErrImageOrISORequired
	case 1:
		// exactly one boot-image source — valid
	default:
		return hcloud.ServerCreateOpts{}, ErrImageAndISOBothSet
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
		// Always set PublicNet explicitly so IPv4-less / IPv6-less nodes are honored.
		// A nil toggle defaults to true, matching Hetzner's default of assigning both.
		PublicNet: &hcloud.ServerCreatePublicNet{
			EnableIPv4: publicNetEnabled(opts.EnableIPv4),
			EnableIPv6: publicNetEnabled(opts.EnableIPv6),
		},
	}

	// Boot from the single configured source: an ISO (Talos public ISOs), a
	// snapshot image by ID, or a stock OS image by name (k3s/kubeadm cloud-init).
	switch {
	case opts.ISOID > 0:
		// When using ISO, we need a placeholder image for the server disk.
		// The server must NOT start automatically so the ISO can be attached
		// before the first boot — otherwise the server boots from the Debian
		// disk image and never enters Talos maintenance mode.
		createOpts.Image = &hcloud.Image{
			Name: "debian-13",
		}
		createOpts.StartAfterCreate = new(false)
	case opts.ImageName != "":
		createOpts.Image = &hcloud.Image{
			Name: opts.ImageName,
		}
	default:
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
