package kwokprovisioner

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/spf13/pflag"

	runner "github.com/devantler-tech/ksail/v6/pkg/runner"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v6/pkg/svc/provisioner/cluster/clustererr"
	"sigs.k8s.io/kwok/pkg/config"
	createcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/create/cluster"
	deletecluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/delete/cluster"
	startcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/start/cluster"
	stopcluster "sigs.k8s.io/kwok/pkg/kwokctl/cmd/stop/cluster"
	kwokruntime "sigs.k8s.io/kwok/pkg/kwokctl/runtime"
	kwoklog "sigs.k8s.io/kwok/pkg/log"

	// Register the Docker compose runtime so kwokctl can find it.
	_ "sigs.k8s.io/kwok/pkg/kwokctl/runtime/compose"
)

// kwokControllerImageVersion is the released KWOK image version to use.
// We pin to v0.7.0 because our Go dependency uses a main-branch
// pseudo-version whose embedded version (v0.8.0) has no published images.
const kwokControllerImageVersion = "v0.7.0"

// kwokControllerImage is the full image reference for the KWOK controller.
const kwokControllerImage = "registry.k8s.io/kwok/kwok:" + kwokControllerImageVersion

// globalMu serialises access to process-global state that kwokctl
// reads/writes (os.Args, config.DefaultCluster). Without this, concurrent
// provisioner calls (e.g. parallel tests or multi-cluster operations)
// could race on these globals.
//
//nolint:gochecknoglobals // kwokctl mutates os.Args and config.DefaultCluster; process-wide mutex required.
var globalMu sync.Mutex

// Provisioner manages KWOK clusters using kwokctl's Cobra commands.
// It uses the docker (compose) runtime which runs etcd + kube-apiserver +
// kwok-controller as Docker containers.
type Provisioner struct {
	name          string
	configPath    string
	infraProvider provider.Provider
	runner        runner.CommandRunner
	stdout        io.Writer
	stderr        io.Writer
}

// NewProvisioner creates a new KWOK provisioner.
// configPath is the optional path to a kwok.yaml configuration file.
func NewProvisioner(
	name string,
	configPath string,
	infraProvider provider.Provider,
) *Provisioner {
	return &Provisioner{
		name:          name,
		configPath:    configPath,
		infraProvider: infraProvider,
		runner:        runner.NewCobraCommandRunner(os.Stdout, os.Stderr),
		stdout:        os.Stdout,
		stderr:        os.Stderr,
	}
}

// SetProvider sets the infrastructure provider for node operations.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// Create creates a KWOK cluster using kwokctl's create command.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	configPath, cleanup, err := p.resolveConfigPath()
	if err != nil {
		return fmt.Errorf("failed to create KWOK cluster: %w", err)
	}

	if cleanup != nil {
		defer cleanup()
	}

	return p.withCluster(ctx, target, func(kwokCtx context.Context) error {
		cmd := createcluster.NewCommand(kwokCtx)

		args := []string{
			"--runtime", "docker",
			"--kwok-controller-image", kwokControllerImage,
		}
		if configPath != "" {
			args = append(args, "--config", configPath)
		}

		_, err := p.runner.Run(kwokCtx, cmd, args)
		if err != nil {
			return fmt.Errorf("failed to create KWOK cluster: %w", err)
		}

		return nil
	})
}

// Delete deletes a KWOK cluster using kwokctl's delete command.
func (p *Provisioner) Delete(ctx context.Context, name string) error {
	target := p.resolveName(name)

	globalMu.Lock()
	defer globalMu.Unlock()

	kwokCtx, err := p.initContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete KWOK cluster: %w", err)
	}

	exists, err := p.existsWithContext(kwokCtx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererr.ErrClusterNotFound, target)
	}

	defer setDefaultCluster(target)()

	cmd := deletecluster.NewCommand(kwokCtx)

	_, err = p.runner.Run(kwokCtx, cmd, []string{})
	if err != nil {
		return fmt.Errorf("failed to delete KWOK cluster: %w", err)
	}

	return nil
}

// Start starts a stopped KWOK cluster.
func (p *Provisioner) Start(ctx context.Context, name string) error {
	return p.withCluster(ctx, p.resolveName(name), func(kwokCtx context.Context) error {
		cmd := startcluster.NewCommand(kwokCtx)

		_, err := p.runner.Run(kwokCtx, cmd, []string{})
		if err != nil {
			return fmt.Errorf("failed to start KWOK cluster: %w", err)
		}

		return nil
	})
}

// Stop stops a running KWOK cluster.
func (p *Provisioner) Stop(ctx context.Context, name string) error {
	return p.withCluster(ctx, p.resolveName(name), func(kwokCtx context.Context) error {
		cmd := stopcluster.NewCommand(kwokCtx)

		_, err := p.runner.Run(kwokCtx, cmd, []string{})
		if err != nil {
			return fmt.Errorf("failed to stop KWOK cluster: %w", err)
		}

		return nil
	})
}

// List returns all KWOK cluster names.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	kwokCtx, err := p.lockAndInit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list KWOK clusters: %w", err)
	}
	defer globalMu.Unlock()

	return p.listWithContext(kwokCtx)
}

// Exists checks if a KWOK cluster exists.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	kwokCtx, err := p.lockAndInit(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check cluster existence: %w", err)
	}
	defer globalMu.Unlock()

	return p.existsWithContext(kwokCtx, p.resolveName(name))
}

// initContext initializes a Go context compatible with kwokctl's internal
// expectations. kwokctl stores configuration objects inside the context via
// config.InitFlags / log.InitFlags, both of which read os.Args directly.
// We temporarily override os.Args so that the kwokctl flag parsers receive
// only the flags relevant to KWOK (e.g. --config <path>).
//
// The caller must hold globalMu.
func (p *Provisioner) initContext(ctx context.Context) (context.Context, error) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	args := []string{"kwokctl"}
	if p.configPath != "" {
		args = append(args, "--config", p.configPath)
	}
	os.Args = args

	flagset := pflag.NewFlagSet("kwokctl", pflag.ContinueOnError)
	flagset.ParseErrorsAllowlist.UnknownFlags = true
	flagset.Usage = func() {}

	ctx, _ = kwoklog.InitFlags(ctx, flagset)

	ctx, err := config.InitFlags(ctx, flagset)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize kwokctl config: %w", err)
	}

	return ctx, nil
}

// withCluster acquires globalMu, initializes the kwokctl context,
// sets config.DefaultCluster for the duration of action, and restores it
// on return.
func (p *Provisioner) withCluster(
	ctx context.Context,
	target string,
	action func(kwokCtx context.Context) error,
) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	kwokCtx, err := p.initContext(ctx)
	if err != nil {
		return err
	}

	defer setDefaultCluster(target)()

	return action(kwokCtx)
}

// lockAndInit acquires globalMu and initializes the kwokctl context.
// The caller must defer globalMu.Unlock().
func (p *Provisioner) lockAndInit(ctx context.Context) (context.Context, error) {
	globalMu.Lock()

	kwokCtx, err := p.initContext(ctx)
	if err != nil {
		globalMu.Unlock()

		return nil, err
	}

	return kwokCtx, nil
}

func (p *Provisioner) listWithContext(kwokCtx context.Context) ([]string, error) {
	clusters, err := kwokruntime.ListClusters(kwokCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to list KWOK clusters: %w", err)
	}

	return clusters, nil
}

func (p *Provisioner) existsWithContext(kwokCtx context.Context, target string) (bool, error) {
	clusters, err := p.listWithContext(kwokCtx)
	if err != nil {
		return false, err
	}

	for _, c := range clusters {
		if c == target {
			return true, nil
		}
	}

	return false, nil
}

func (p *Provisioner) resolveName(name string) string {
	if name != "" {
		return name
	}

	return p.name
}

// setDefaultCluster sets config.DefaultCluster and returns a function that
// restores the previous value. The caller must hold globalMu.
func setDefaultCluster(name string) func() {
	prev := config.DefaultCluster
	config.DefaultCluster = name

	return func() { config.DefaultCluster = prev }
}

// resolveConfigPath returns the config file path to pass to kwokctl.
// If an explicit configPath was provided, it is returned as-is.
// Otherwise a temporary file containing the default simulation CRDs
// is created and a cleanup function is returned that removes it.
func (p *Provisioner) resolveConfigPath() (string, func(), error) {
	if p.configPath != "" {
		return p.configPath, nil, nil
	}

	tmpFile, err := os.CreateTemp("", "kwok-default-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp config: %w", err)
	}

	tmpName := tmpFile.Name()

	_, writeErr := tmpFile.WriteString(defaultSimulationConfig)
	if writeErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)

		return "", nil, fmt.Errorf("failed to write temp config: %w", writeErr)
	}

	closeErr := tmpFile.Close()
	if closeErr != nil {
		_ = os.Remove(tmpName)

		return "", nil, fmt.Errorf("failed to close temp config: %w", closeErr)
	}

	cleanup := func() { _ = os.Remove(tmpName) }

	return tmpName, cleanup, nil
}

// defaultSimulationConfig contains the KWOK simulation CRDs that are NOT
// provided by KWOK by default. These enable kubectl logs, exec, attach,
// and port-forward to work out of the box on simulated pods.
const defaultSimulationConfig = `apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterLogs
metadata:
  name: default-logs
spec:
  selector: {}
  logs:
    - containers:
        - name: '*'
      logsFile: /dev/null
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterExec
metadata:
  name: default-exec
spec:
  selector: {}
  execs:
    - containers:
        - name: '*'
      command:
        - /bin/sh
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterAttach
metadata:
  name: default-attach
spec:
  selector: {}
  attaches:
    - containers:
        - name: '*'
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterPortForward
metadata:
  name: default-port-forward
spec:
  selector: {}
  forwards:
    - ports:
        - name: '*'
`
