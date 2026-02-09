package kindprovisioner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/devantler-tech/ksail/v5/pkg/io/marshaller"
	runner "github.com/devantler-tech/ksail/v5/pkg/runner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/detector"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
	kindcmd "sigs.k8s.io/kind/pkg/cmd"
	createcluster "sigs.k8s.io/kind/pkg/cmd/kind/create/cluster"
	deletecluster "sigs.k8s.io/kind/pkg/cmd/kind/delete/cluster"
	getclusters "sigs.k8s.io/kind/pkg/cmd/kind/get/clusters"
	"sigs.k8s.io/kind/pkg/log"
)

// This allows kind's console output to be displayed in real-time.
// Only info-level messages (V(0)) are enabled to avoid verbose debug output.
type streamLogger struct {
	writer io.Writer
}

func (l *streamLogger) Warn(message string) {
	l.write(message)
}

func (l *streamLogger) Warnf(format string, args ...any) {
	l.write(fmt.Sprintf(format, args...))
}

func (l *streamLogger) Error(message string) {
	l.write(message)
}

func (l *streamLogger) Errorf(format string, args ...any) {
	l.write(fmt.Sprintf(format, args...))
}

// noopInfoLogger discards verbose/debug messages (V(1) and higher).
type noopInfoLogger struct{}

func (noopInfoLogger) Info(string)          {}
func (noopInfoLogger) Infof(string, ...any) {}
func (noopInfoLogger) Enabled() bool        { return false }

func (l *streamLogger) V(level log.Level) log.InfoLogger {
	// Only enable info-level messages (V(0)), suppress verbose/debug (V(1+))
	if level > 0 {
		return noopInfoLogger{}
	}

	return l
}

func (l *streamLogger) Info(message string) {
	l.write(message)
}

func (l *streamLogger) Infof(format string, args ...any) {
	l.write(fmt.Sprintf(format, args...))
}

func (l *streamLogger) Enabled() bool {
	return true
}

func (l *streamLogger) write(message string) {
	if l == nil {
		return
	}

	if message == "" {
		_, _ = io.WriteString(l.writer, "\n")

		return
	}

	if strings.ContainsRune(message, '\r') || strings.HasSuffix(message, "\n") {
		_, _ = io.WriteString(l.writer, message)

		return
	}

	_, _ = io.WriteString(l.writer, message+"\n")
}

// KindProvider describes the subset of methods from kind's Provider used here.
type KindProvider interface {
	Create(name string, opts ...cluster.CreateOption) error
	Delete(name, kubeconfigPath string) error
	List() ([]string, error)
	ListNodes(name string) ([]string, error)
}

// KindClusterProvisioner is an implementation of the ClusterProvisioner interface for provisioning kind clusters.
// It uses kind's Cobra commands where available (create, delete, list) and delegates
// infrastructure operations (start, stop) to the injected Provider.
type KindClusterProvisioner struct {
	kubeConfig        string
	kindConfig        *v1alpha4.Cluster
	kindSDKProvider   KindProvider
	infraProvider     provider.Provider
	runner            runner.CommandRunner
	componentDetector *detector.ComponentDetector
}

// NewKindClusterProvisioner constructs a KindClusterProvisioner with explicit dependencies
// for the kind SDK provider and infrastructure provider.
func NewKindClusterProvisioner(
	kindConfig *v1alpha4.Cluster,
	kubeConfig string,
	kindSDKProvider KindProvider,
	infraProvider provider.Provider,
) *KindClusterProvisioner {
	return NewKindClusterProvisionerWithRunner(
		kindConfig,
		kubeConfig,
		kindSDKProvider,
		infraProvider,
		runner.NewCobraCommandRunner(os.Stdout, os.Stderr),
	)
}

// NewKindClusterProvisionerWithRunner constructs a KindClusterProvisioner with
// an explicit command runner for testing purposes.
func NewKindClusterProvisionerWithRunner(
	kindConfig *v1alpha4.Cluster,
	kubeConfig string,
	kindSDKProvider KindProvider,
	infraProvider provider.Provider,
	runner runner.CommandRunner,
) *KindClusterProvisioner {
	return &KindClusterProvisioner{
		kubeConfig:      kubeConfig,
		kindConfig:      kindConfig,
		kindSDKProvider: kindSDKProvider,
		infraProvider:   infraProvider,
		runner:          runner,
	}
}

// SetProvider sets the infrastructure provider for node operations.
// This implements the ProviderAware interface.
func (k *KindClusterProvisioner) SetProvider(p provider.Provider) {
	k.infraProvider = p
}

// WithComponentDetector sets the component detector for querying cluster state.
func (k *KindClusterProvisioner) WithComponentDetector(d *detector.ComponentDetector) {
	k.componentDetector = d
}

// Create creates a kind cluster using kind's Cobra command.
func (k *KindClusterProvisioner) Create(ctx context.Context, name string) error {
	target := setName(name, k.kindConfig.Name)

	// Serialize config to temp file (required by kind's Cobra command)
	tmpFile, err := os.CreateTemp("", "kind-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}

	defer func() { _ = tmpFile.Close() }()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	marshaller := marshaller.NewYAMLMarshaller[*v1alpha4.Cluster]()

	configYAML, err := marshaller.Marshal(k.kindConfig)
	if err != nil {
		return fmt.Errorf("marshal kind config: %w", err)
	}

	const configFilePerms = 0o600

	err = os.WriteFile(tmpFile.Name(), []byte(configYAML), configFilePerms)
	if err != nil {
		return fmt.Errorf("write temp config file: %w", err)
	}

	// Kind writes output through its logger interface - send directly to stdout
	logger := &streamLogger{writer: os.Stdout}

	// Set up IOStreams - kind commands may also write here
	streams := kindcmd.IOStreams{
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	cmd := createcluster.NewCommand(logger, streams)

	args := []string{"--name", target, "--config", tmpFile.Name()}

	_, err = k.runner.Run(ctx, cmd, args)
	if err != nil {
		return fmt.Errorf("failed to create kind cluster: %w", err)
	}

	return nil
}

// Delete deletes a kind cluster using kind's Cobra command.
// Returns clustererrors.ErrClusterNotFound if the cluster does not exist.
func (k *KindClusterProvisioner) Delete(ctx context.Context, name string) error {
	target := setName(name, k.kindConfig.Name)

	// Check if cluster exists before attempting to delete
	exists, err := k.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererrors.ErrClusterNotFound, target)
	}

	kubeconfigPath, err := iopath.ExpandHomePath(k.kubeConfig)
	if err != nil {
		return fmt.Errorf("failed to expand kubeconfig path: %w", err)
	}

	// Kind writes output through its logger interface - send directly to stdout
	logger := &streamLogger{writer: os.Stdout}

	// Set up IOStreams - kind commands may also write here
	streams := kindcmd.IOStreams{
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}

	cmd := deletecluster.NewCommand(logger, streams)

	args := []string{"--name", target}
	if kubeconfigPath != "" {
		args = append(args, "--kubeconfig", kubeconfigPath)
	}

	_, err = k.runner.Run(ctx, cmd, args)
	if err != nil {
		return fmt.Errorf("failed to delete kind cluster: %w", err)
	}

	return nil
}

// Start starts a kind cluster.
// Delegates to the infrastructure provider for container operations.
func (k *KindClusterProvisioner) Start(ctx context.Context, name string) error {
	return k.withProvider(ctx, name, "start", k.infraProvider.StartNodes)
}

// Stop stops a kind cluster.
// Delegates to the infrastructure provider for container operations.
func (k *KindClusterProvisioner) Stop(ctx context.Context, name string) error {
	return k.withProvider(ctx, name, "stop", k.infraProvider.StopNodes)
}

// List returns all kind clusters using kind's Cobra command.
func (k *KindClusterProvisioner) List(ctx context.Context) ([]string, error) {
	// Use a buffer to capture output without displaying it
	var outBuf bytes.Buffer

	// Kind writes output through its logger interface - capture to buffer
	logger := &streamLogger{writer: &outBuf}

	// Set up IOStreams - capture kind commands output to buffer
	// Note: Kind's getclusters command writes to streams.Out directly (via fmt.Fprintln),
	// not through cmd.SetOut(), so we read from outBuf primarily.
	streams := kindcmd.IOStreams{
		Out:    &outBuf,
		ErrOut: io.Discard,
	}

	cmd := getclusters.NewCommand(logger, streams)

	result, err := k.runner.Run(ctx, cmd, []string{})
	if err != nil {
		return nil, fmt.Errorf("failed to list kind clusters: %w", err)
	}

	const noKindClustersMsg = "No kind clusters found."

	// Parse output - Kind writes cluster names via fmt.Fprintln(streams.Out, ...)
	// which goes to outBuf. If outBuf is empty (e.g., in mocked tests), fall back
	// to result.Stdout for compatibility.
	output := outBuf.Bytes()
	if len(output) == 0 {
		output = []byte(result.Stdout)
	}

	lines := bytes.Split(output, []byte("\n"))

	var clusters []string

	for _, line := range lines {
		name := string(bytes.TrimSpace(line))
		if name != "" && name != noKindClustersMsg {
			clusters = append(clusters, name)
		}
	}

	return clusters, nil
}

// Exists checks if a kind cluster exists.
func (k *KindClusterProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusters, err := k.List(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list kind clusters: %w", err)
	}

	target := setName(name, k.kindConfig.Name)

	if slices.Contains(clusters, target) {
		return true, nil
	}

	return false, nil
}

// --- internals ---

// withProvider executes a provider operation with proper nil check and error wrapping.
func (k *KindClusterProvisioner) withProvider(
	ctx context.Context,
	name string,
	operationName string,
	providerFunc func(ctx context.Context, clusterName string) error,
) error {
	target := setName(name, k.kindConfig.Name)

	if k.infraProvider == nil {
		return fmt.Errorf("%w for cluster '%s'", clustererrors.ErrProviderNotSet, target)
	}

	err := providerFunc(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to %s cluster '%s': %w", operationName, target, err)
	}

	return nil
}

func setName(name string, kindConfigName string) string {
	target := name
	if target == "" {
		target = kindConfigName
	}

	return target
}
