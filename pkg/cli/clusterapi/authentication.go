package clusterapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/awssso"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// Authentication probes the selected context's credential command without starting sign-in.
func (s *Service) Authentication(
	ctx context.Context,
	_, name string,
) (api.ClusterAuthentication, error) {
	target, err := s.authenticationTarget(ctx, name)
	if errors.Is(err, awssso.ErrUnsupported) {
		return api.ClusterAuthentication{Message: err.Error()}, nil
	}

	if err != nil {
		return api.ClusterAuthentication{}, err
	}

	expired, err := target.Expired(ctx)
	if err != nil {
		return api.ClusterAuthentication{}, fmt.Errorf("check selected AWS SSO session: %w", err)
	}

	return api.ClusterAuthentication{Supported: true, Required: expired}, nil
}

// RenewAuthentication performs only the selected context's AWS SSO sign-in, never a cluster mutation.
func (s *Service) RenewAuthentication(ctx context.Context, _, name string) error {
	target, err := s.authenticationTarget(ctx, name)
	if err != nil {
		return err
	}

	err = s.ssoRenewal.Renew(ctx, target)
	if err != nil {
		return fmt.Errorf("renew selected AWS SSO session: %w", err)
	}

	return nil
}

func (s *Service) authenticationTarget(ctx context.Context, name string) (*awssso.Target, error) {
	config, err := s.restConfigForCluster(ctx, name)
	if err != nil {
		return nil, err
	}

	target, err := awssso.Resolve(ctx, config.ExecProvider)
	if err != nil {
		return nil, fmt.Errorf("resolve selected authentication: %w", err)
	}

	return target, nil
}
