package hetzner

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ErrISOUnavailable indicates that the selected ISO cannot be resolved by ID.
var ErrISOUnavailable = errors.New("configured ISO is unavailable")

// ErrISOArchitectureMismatch prevents replacing a node with incompatible media.
var ErrISOArchitectureMismatch = errors.New(
	"ISO architecture does not match replacement server type",
)

// ValidateISO checks the selected boot ISO without creating or changing cloud
// resources. Availability now does not guarantee a later attach will succeed.
func (p *Provider) ValidateISO(ctx context.Context, isoID int64, serverTypeName string) error {
	if p.client == nil {
		return provider.ErrProviderUnavailable
	}

	if isoID <= 0 {
		return ErrISOUnavailable
	}

	iso, _, err := p.client.ISO.GetByID(ctx, isoID)
	if err != nil {
		return fmt.Errorf("look up replacement ISO: %w", err)
	}

	if iso == nil || iso.ID != isoID {
		return ErrISOUnavailable
	}

	return p.validateISOArchitecture(ctx, iso, serverTypeName)
}

func (p *Provider) validateISOArchitecture(
	ctx context.Context, iso *hcloud.ISO, serverTypeName string,
) error {
	// Custom ISOs explicitly support an architecture wildcard in the API.
	if iso.Architecture == nil && iso.Type == "custom" {
		return nil
	}

	serverType, _, err := p.client.ServerType.GetByName(ctx, serverTypeName)
	if err != nil {
		return fmt.Errorf("look up replacement server type: %w", err)
	}

	if iso.Architecture == nil || serverType == nil ||
		serverType.Name != serverTypeName || serverType.Architecture == "" ||
		*iso.Architecture != serverType.Architecture {
		return ErrISOArchitectureMismatch
	}

	return nil
}
