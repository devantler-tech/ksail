package hetzner

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
)

// ErrISOUnavailable indicates that the selected ISO cannot be resolved by ID.
var ErrISOUnavailable = errors.New("configured ISO is unavailable")

// ValidateISO checks the selected boot ISO without creating or changing cloud
// resources. Availability now does not guarantee a later attach will succeed.
func (p *Provider) ValidateISO(ctx context.Context, isoID int64) error {
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

	return nil
}
