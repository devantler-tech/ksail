package talosprovisioner

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/imagefactorytransport"
	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	factoryclient "github.com/siderolabs/image-factory/pkg/client"
)

// imageFactoryURL is the Image Factory that serves KSail's installer and snapshot images.
const imageFactoryURL = "https://factory.talos.dev"

// imageFactoryRegistrationTimeout bounds all schematic registration attempts, so a stalled
// Image Factory response cannot block an upgrade or snapshot build indefinitely. The factory
// client uses an http.Client with no timeout of its own.
const imageFactoryRegistrationTimeout = 30 * time.Second

const (
	imageFactoryRegistrationAttempts = 3
	imageFactoryRegistrationBackoff  = 100 * time.Millisecond
	imageFactoryRegistrationMaxWait  = 200 * time.Millisecond
)

// ErrSchematicIDMismatch reports that Image Factory stored KSail's schematic under an ID other
// than the one KSail computed, so images addressed by the computed ID would not be served.
var ErrSchematicIDMismatch = errors.New("image factory stored the schematic under a different ID")

// registerWithImageFactory sends a schematic to Image Factory and returns its stored ID.
func registerWithImageFactory(
	ctx context.Context,
	computed talosconfigmanager.Schematic,
) (string, error) {
	return registerSchematic(ctx, imageFactoryURL, imageFactoryRegistrationTimeout, computed)
}

// registerSchematic sends a schematic to the Image Factory at baseURL, bounding the request by
// timeout while still honouring any earlier cancellation or deadline on ctx.
func registerSchematic(
	ctx context.Context,
	baseURL string,
	timeout time.Duration,
	computed talosconfigmanager.Schematic,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpClient := imagefactorytransport.Client()
	defer httpClient.CloseIdleConnections()
	client, err := factoryclient.New(baseURL, factoryclient.WithClient(httpClient))
	if err != nil {
		return "", fmt.Errorf("create image factory client: %w", err)
	}

	var schematicID string

	err = netretry.Do(ctx, imageFactoryRegistrationAttempts,
		imageFactoryRegistrationBackoff, imageFactoryRegistrationMaxWait,
		func() error {
			var createErr error

			schematicID, _, createErr = client.SchematicCreate(ctx, computed)
			if createErr != nil {
				return fmt.Errorf("create schematic: %w", createErr)
			}

			return nil
		}, netretry.WithRetryable(isSchematicRegistrationRetryable))
	if err != nil {
		return "", fmt.Errorf("register schematic with image factory: %w", err)
	}

	return schematicID, nil
}

// Status codes, not response prose, decide whether a factory refusal is transient.
func isSchematicRegistrationRetryable(err error) bool {
	if responseError, ok := errors.AsType[*factoryclient.HTTPError](err); ok {
		return responseError.Code == http.StatusTooManyRequests ||
			(responseError.Code >= http.StatusInternalServerError && responseError.Code < 600)
	}

	_, certificateFailure := errors.AsType[*tls.CertificateVerificationError](err)
	if factoryclient.IsInvalidSchematicError(err) || certificateFailure {
		return false
	}

	return netretry.IsRetryable(err)
}

// ensureSchematicRegistered registers KSail's computed schematic with Image Factory before
// schematicID is used for an image, because the factory serves images only for schematics it
// has been sent (#7132). Registration is idempotent, and the factory must store it under the
// computed ID. An explicitly configured schematicId that KSail did not compute is left as it is.
func (p *Provisioner) ensureSchematicRegistered(ctx context.Context, schematicID string) error {
	if p.talosConfigs == nil || p.talosConfigs.Schematic() == nil ||
		schematicID != p.talosConfigs.SchematicID() {
		return nil
	}

	registeredID, err := p.schematicRegistrar(ctx, *p.talosConfigs.Schematic())
	if err != nil {
		return fmt.Errorf("registering Talos schematic %s: %w", schematicID, err)
	}

	if registeredID != schematicID {
		return fmt.Errorf(
			"%w: computed %s, image factory returned %q",
			ErrSchematicIDMismatch, schematicID, registeredID,
		)
	}

	return nil
}
