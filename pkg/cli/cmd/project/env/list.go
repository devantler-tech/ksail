package env

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/spf13/cobra"
)

// Output formats accepted by --output. Defined locally so this command does not
// couple to the cluster package's private output helpers.
const (
	listEnvOutputText = "text"
	listEnvOutputJSON = "json"
)

// tabwriter geometry for the text table (minwidth 0, tab/padding 2, space pad).
const (
	listEnvTabPadding = 2
	listEnvTabSize    = 2
)

// ErrInvalidOutputFormat is returned when --output is neither text nor json.
var ErrInvalidOutputFormat = errors.New("invalid output format")

const listEnvironmentsLongDesc = `List the cluster environments declared in the workspace.

An environment is a ksail.<name>.yaml root config in the workspace root (the same
convention "project env add"'s --from resolves against); the base ksail.yaml
is not an environment. Each declared environment is reported with its distribution
and provider, read from its config. A config that fails to load is skipped so a
single malformed file never hides the environments that do load.

Output Format:
  NAME     DISTRIBUTION   PROVIDER   CONFIG
  prod     Talos          Hetzner    ksail.prod.yaml
  staging  K3s            Docker     ksail.staging.yaml

Use --output json for machine-readable output. The JSON is an array of objects:
  [
    {"name": "prod", "distribution": "Talos", "provider": "Hetzner", "config": "ksail.prod.yaml"}
  ]

Examples:
  # List the declared environments
  ksail project env list

  # Machine-readable JSON
  ksail project env list --output json`

// NewListCmd creates and returns the `project env list` command (formerly
// `project list-environments`; the deprecated alias delegates here).
//
// It is a read-only enumeration of already-declared workspace state with no side
// effects, so it ships visible rather than behind a feature flag — the
// low-risk-additive carve-out to the feature-flag-first default, matching its
// sibling `env add` and the analogous cluster list, both of which are
// unflagged.
func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List the cluster environments declared in the workspace",
		Long:         listEnvironmentsLongDesc,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return HandleListRunE(cmd)
		},
	}

	cmd.Flags().String(
		"output", listEnvOutputText,
		"Output format: text or json. Use json for machine-readable output "+
			"(array of {name, distribution, provider, config}).",
	)

	return cmd
}

// environmentResult is the machine-readable shape emitted by --output json. It is
// the CLI projection of environment.Environment (the ConfigFile field is renamed
// "config" for the wire format).
type environmentResult struct {
	Name         string `json:"name"`
	Distribution string `json:"distribution"`
	Provider     string `json:"provider"`
	Config       string `json:"config"`
}

// HandleListRunE handles the `project env list` command. It resolves the
// workspace root, enumerates the declared environments via
// environment.DeriveEnvironments (loading each config the same silent,
// validation-skipping way `env add` does), and renders them as a table or
// JSON. Exported for testing.
func HandleListRunE(cmd *cobra.Command) error {
	output, _ := cmd.Flags().GetString("output")
	if output != listEnvOutputText && output != listEnvOutputJSON {
		return fmt.Errorf(
			"%w: %q (use %q or %q)",
			ErrInvalidOutputFormat, output, listEnvOutputText, listEnvOutputJSON,
		)
	}

	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Canonicalise so the workspace root matches the symlink-resolved paths the
	// config loader derives, mirroring `env add`.
	repoRoot, err := fsutil.EvalCanonicalPath(workDir)
	if err != nil {
		return fmt.Errorf("failed to resolve current directory: %w", err)
	}

	loader := func(configFile string) (*v1alpha1.Cluster, error) {
		return loadEnvironmentConfig(cmd, configFile)
	}

	envs, err := environment.DeriveEnvironments(repoRoot, loader)
	if err != nil {
		return fmt.Errorf("discovering environments: %w", err)
	}

	if output == listEnvOutputJSON {
		return emitEnvironmentsJSON(cmd.OutOrStdout(), envs)
	}

	displayEnvironments(cmd.OutOrStdout(), envs)

	return nil
}

// emitEnvironmentsJSON writes the environments as a JSON array (always an array,
// "[]" when none) so downstream tooling and the web UI can consume a stable shape.
func emitEnvironmentsJSON(out io.Writer, envs []environment.Environment) error {
	results := make([]environmentResult, 0, len(envs))
	for _, env := range envs {
		results = append(results, environmentResult{
			Name:         env.Name,
			Distribution: string(env.Distribution),
			Provider:     string(env.Provider),
			Config:       env.ConfigFile,
		})
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(results)
	if err != nil {
		return fmt.Errorf("encoding environments as JSON: %w", err)
	}

	return nil
}

// displayEnvironments prints an aligned table of the declared environments, or a
// friendly hint when none are declared.
func displayEnvironments(out io.Writer, envs []environment.Environment) {
	if len(envs) == 0 {
		notify.Infof(
			out,
			"no environments declared; scaffold one with "+
				"`ksail project env add <name> --from <env>`",
		)

		return
	}

	writer := tabwriter.NewWriter(out, 0, listEnvTabSize, listEnvTabPadding, ' ', 0)

	_, _ = fmt.Fprintln(writer, "NAME\tDISTRIBUTION\tPROVIDER\tCONFIG")

	for _, env := range envs {
		_, _ = fmt.Fprintf(
			writer, "%s\t%s\t%s\t%s\n",
			env.Name, env.Distribution, env.Provider, env.ConfigFile,
		)
	}

	_ = writer.Flush()
}
