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

// kwokGlobalMu serialises access to process-global state that kwokctl
// reads/writes (os.Args, config.DefaultCluster). Without this, concurrent
// provisioner calls (e.g. parallel tests or multi-cluster operations)
// could race on these globals.
var kwokGlobalMu sync.Mutex

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

// kwokControllerImageVersion is the released KWOK image version to use.
// We pin to v0.7.0 because our Go dependency uses a main-branch
// pseudo-version whose embedded version (v0.8.0) has no published images.
const kwokControllerImageVersion = "v0.7.0"

// kwokControllerImage is the full image reference for the KWOK controller.
const kwokControllerImage = "registry.k8s.io/kwok/kwok:" + kwokControllerImageVersion

// initContext initializes a Go context compatible with kwokctl's internal
// expectations. kwokctl stores configuration objects inside the context via
// config.InitFlags / log.InitFlags, both of which read os.Args directly.
// We temporarily override os.Args so that the kwokctl flag parsers receive
// only the flags relevant to KWOK (e.g. --config <path>).
//
// The caller must hold kwokGlobalMu.
func (p *Provisioner) initContext(ctx context.Context) (context.Context, error) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	args := []string{"kwokctl"}
	if p.configPath != "" {
		args = append(args, "--config", p.configPath)
	}
	os.Args = args

	flagset := pflag.NewFlagSet("kwokctl", pflag.ContinueOnError)
	flagset.ParseErrorsWhitelist.UnknownFlags = true
	flagset.Usage = func() {}

	ctx, _ = kwoklog.InitFlags(ctx, flagset)

	ctx, err := config.InitFlags(ctx, flagset)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize kwokctl config: %w", err)
	}

	return ctx, nil
}

// setDefaultCluster sets config.DefaultCluster and returns a function that
// restores the previous value. The caller must hold kwokGlobalMu.
func setDefaultCluster(name string) func() {
	prev := config.DefaultCluster
	config.DefaultCluster = name

	return func() { config.DefaultCluster = prev }
}

// SetProvider sets the infrastructure provider for node operations.
func (p *Provisioner) SetProvider(prov provider.Provider) {
	p.infraProvider = prov
}

// withCluster acquires kwokGlobalMu, initializes the kwokctl context,
// sets config.DefaultCluster for the duration of fn, and restores it on return.
func (p *Provisioner) withCluster(
	ctx context.Context,
	target string,
	fn func(kwokCtx context.Context) error,
) error {
	kwokGlobalMu.Lock()
	defer kwokGlobalMu.Unlock()

	kwokCtx, err := p.initContext(ctx)
	if err != nil {
		return err
	}

	defer setDefaultCluster(target)()

	return fn(kwokCtx)
}

// Create creates a KWOK cluster using kwokctl's create command.
func (p *Provisioner) Create(ctx context.Context, name string) error {
	target := p.resolveName(name)

	return p.withCluster(ctx, target, func(kwokCtx context.Context) error {
		cmd := createcluster.NewCommand(kwokCtx)

		args := []string{
			"--runtime", "docker",
			"--kwok-controller-image", kwokControllerImage,
		}
		if p.configPath != "" {
			args = append(args, "--config", p.configPath)
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

	kwokGlobalMu.Lock()
	defer kwokGlobalMu.Unlock()

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

// withLock acquires kwokGlobalMu and initializes the kwokctl context.
func (p *Provisioner) withLock(ctx context.Context) (context.Context, error) {
	kwokGlobalMu.Lock()

	kwokCtx, err := p.initContext(ctx)
	if err != nil {
		kwokGlobalMu.Unlock()

		return nil, err
	}

	return kwokCtx, nil
}

// List returns all KWOK cluster names.
func (p *Provisioner) List(ctx context.Context) ([]string, error) {
	kwokCtx, err := p.withLock(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list KWOK clusters: %w", err)
	}
	defer kwokGlobalMu.Unlock()

	return p.listWithContext(kwokCtx)
}

// Exists checks if a KWOK cluster exists.
func (p *Provisioner) Exists(ctx context.Context, name string) (bool, error) {
	kwokCtx, err := p.withLock(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check cluster existence: %w", err)
	}
	defer kwokGlobalMu.Unlock()

	return p.existsWithContext(kwokCtx, p.resolveName(name))
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
