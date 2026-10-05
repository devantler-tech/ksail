package workload

import (
	"errors"
	"fmt"
	"slices"

	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/confirm"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/awssso"
	"github.com/spf13/cobra"
)

func maybeRenewAWSSSO(cmd *cobra.Command) error {
	enabled, err := flags.IsExperimentalEnabled(cmd)
	if err != nil {
		return fmt.Errorf("read experimental setting: %w", err)
	}

	if !enabled || !confirm.IsTTY() ||
		!slices.Contains([]string{"get", "describe", "logs", "explain", "wait"}, cmd.Name()) {
		return nil
	}

	return renewSelectedAWSSSO(cmd)
}

func commandSSOTarget(cmd *cobra.Command) (*awssso.Target, error) {
	path, err := cmd.Flags().GetString("kubeconfig")
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig for sign-in: %w", err)
	}

	contextName, err := cmd.Flags().GetString("context")
	if err != nil {
		return nil, fmt.Errorf("resolve context for sign-in: %w", err)
	}

	config, err := k8s.BuildRESTConfig(path, contextName)
	if err != nil {
		return nil, fmt.Errorf("load selected context for sign-in: %w", err)
	}

	target, err := awssso.Resolve(cmd.Context(), config.ExecProvider)
	if err != nil {
		return nil, fmt.Errorf("resolve selected AWS sign-in: %w", err)
	}

	return target, nil
}

var errSSOSignInCancelled = errors.New("AWS SSO sign-in cancelled")

func renewSelectedAWSSSO(cmd *cobra.Command) error {
	target, err := commandSSOTarget(cmd)
	if errors.Is(err, awssso.ErrUnsupported) {
		return nil // Other credential plugins retain their normal execution path.
	}

	if err != nil {
		return fmt.Errorf("prepare AWS SSO renewal: %w", err)
	}

	expired, err := target.Expired(cmd.Context())
	if err != nil {
		return fmt.Errorf("check selected AWS SSO session: %w", err)
	}

	if !expired {
		return nil
	}

	_, err = fmt.Fprint(cmd.ErrOrStderr(),
		"AWS SSO has expired for the selected context. Type \"yes\" to open AWS sign-in: ")
	if err != nil {
		return fmt.Errorf("show AWS sign-in confirmation: %w", err)
	}

	if !confirm.PromptForConfirmation(cmd.ErrOrStderr()) {
		return errSSOSignInCancelled
	}

	var manager awssso.Manager

	err = manager.Renew(cmd.Context(), target)
	if err != nil {
		return fmt.Errorf("renew selected context authentication: %w", err)
	}
	// Renewal happens before kubectl runs: no partially executed command or mutation is replayed.
	return nil
}
