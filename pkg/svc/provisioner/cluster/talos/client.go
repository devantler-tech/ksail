package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
)

// applyConfigWithMode applies configuration to a single node with the specified mode.
func (p *Provisioner) applyConfigWithMode(
	ctx context.Context,
	nodeIP string,
	config talosconfig.Provider,
	mode machineapi.ApplyConfigurationRequest_Mode,
) error {
	if config == nil {
		return clustererr.ErrConfigNil
	}

	cfgBytes, err := config.Bytes()
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	talosClient, err := p.createTalosClient(ctx, nodeIP)
	if err != nil {
		return err
	}

	defer talosClient.Close() //nolint:errcheck

	_, err = talosClient.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: cfgBytes,
		Mode: mode,
	})
	if err != nil {
		return fmt.Errorf("failed to apply configuration: %w", err)
	}

	return nil
}

// errSavedTalosconfigUnavailable signals that the on-disk talosconfig
// was absent or unreadable. Callers can use [errors.Is] to distinguish
// this expected case from a real client-creation failure and fall
// through to the in-memory PKI bundle.
var errSavedTalosconfigUnavailable = errors.New("saved talosconfig unavailable")

// createTalosClient creates a Talos client for the given node.
// It prefers the saved talosconfig on disk (written during cluster creation)
// because it contains the CA and client certificates the running cluster trusts.
// The in-memory talosConfigs bundle may hold freshly generated PKI that the
// cluster has never seen, so it is used only as a fallback.
func (p *Provisioner) createTalosClient(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, error) {
	client, fromSavedConfig, err := p.createTalosClientFromSavedConfig(ctx, nodeIP)
	if err != nil {
		return nil, err
	}

	if fromSavedConfig {
		return client, nil
	}

	return p.createTalosClientFromBundle(ctx, nodeIP)
}

func (p *Provisioner) createTalosClientFromSavedConfig(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, bool, error) {
	// Prefer the saved talosconfig (written during cluster creation).
	talosconfigPath, pathErr := canonicalSavedTalosconfigPath(p.options.TalosconfigPath)
	if pathErr != nil {
		if !errors.Is(pathErr, errSavedTalosconfigUnavailable) {
			return nil, false, pathErr
		}

		return nil, false, nil
	}

	if talosconfigPath == "" {
		return nil, false, nil
	}

	client, err := p.tryClientFromSavedConfig(ctx, talosconfigPath, nodeIP)
	if err == nil {
		return client, true, nil
	}

	if errors.Is(err, errSavedTalosconfigUnavailable) {
		return nil, false, nil
	}

	return nil, false, err
}

func (p *Provisioner) createTalosClientFromBundle(
	ctx context.Context,
	nodeIP string,
) (*talosclient.Client, error) {
	// Fallback: use the in-memory bundle's TalosConfig (works for first-time creation).
	if p.talosConfigs != nil && p.talosConfigs.Bundle() != nil {
		if talosConf := p.talosConfigs.Bundle().TalosConfig(); talosConf != nil {
			client, err := talosclient.New(ctx,
				talosclient.WithEndpoints(nodeIP),
				talosclient.WithConfig(talosConf),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create Talos client with config: %w", err)
			}

			return client, nil
		}
	}

	return nil, clustererr.ErrTalosConfigRequired
}

// canonicalSavedTalosconfigPath returns the canonical on-disk talosconfig path
// when available. A missing path is treated as unavailable so callers can
// fall back to in-memory configuration.
func canonicalSavedTalosconfigPath(rawPath string) (string, error) {
	expandedPath, expandErr := fsutil.ExpandHomePath(rawPath)
	if expandErr != nil {
		return "", fmt.Errorf("%w: %w", errSavedTalosconfigUnavailable, expandErr)
	}

	canonicalPath, canonErr := fsutil.EvalCanonicalPath(expandedPath)
	if canonErr != nil {
		if errors.Is(canonErr, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %w", errSavedTalosconfigUnavailable, canonErr)
		}

		return "", fmt.Errorf(
			"failed to canonicalize talosconfig path %q: %w",
			expandedPath,
			canonErr,
		)
	}

	return canonicalPath, nil
}

// tryClientFromSavedConfig attempts to construct a Talos client from a
// saved talosconfig at talosconfigPath. It returns
// [errSavedTalosconfigUnavailable] when the file cannot be opened so
// the caller can fall through to the in-memory bundle.
func (p *Provisioner) tryClientFromSavedConfig(
	ctx context.Context,
	talosconfigPath, nodeIP string,
) (*talosclient.Client, error) {
	savedCfg, openErr := clientconfig.Open(talosconfigPath)
	if openErr != nil {
		return nil, fmt.Errorf("%w: %w", errSavedTalosconfigUnavailable, openErr)
	}

	caErr := validateCurrentContextCA(savedCfg, talosconfigPath)
	if caErr != nil {
		return nil, caErr
	}

	client, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeIP),
		talosclient.WithConfig(savedCfg),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client from saved config: %w", err)
	}

	return client, nil
}
