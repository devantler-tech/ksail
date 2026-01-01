package k3dprovisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	runner "github.com/devantler-tech/ksail/v5/pkg/utils/runner"
	clustercommand "github.com/k3d-io/k3d/v5/cmd/cluster"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	// listMutex protects concurrent access to os.Stdout during List operations.
	// This is required because k3d writes directly to os.Stdout before Cobra's output redirection takes effect.
	listMutex sync.Mutex //nolint:gochecknoglobals // Required for thread-safe stdout manipulation

	// logrusConfigOnce ensures logrus is configured exactly once to avoid data races.
	logrusConfigOnce sync.Once //nolint:gochecknoglobals // Required for one-time logrus initialization
)

// K3dClusterProvisioner executes k3d lifecycle commands via Cobra.
type K3dClusterProvisioner struct {
	simpleCfg  *v1alpha5.SimpleConfig
	configPath string
	runner     runner.CommandRunner
}

// NewK3dClusterProvisioner constructs a new command-backed provisioner.
func NewK3dClusterProvisioner(
	simpleCfg *v1alpha5.SimpleConfig,
	configPath string,
) *K3dClusterProvisioner {
	// Configure logrus for k3d's console output once
	// k3d uses logrus for logging, so we need to set it up properly
	// Use sync.Once to prevent data races when called from parallel tests
	logrusConfigOnce.Do(func() {
		logrus.SetOutput(os.Stdout)
		logrus.SetFormatter(&logrus.TextFormatter{
			ForceColors:      true,
			DisableTimestamp: false,
			FullTimestamp:    false,
			TimestampFormat:  "2006-01-02T15:04:05Z",
		})
		logrus.SetLevel(logrus.InfoLevel)
	})

	prov := &K3dClusterProvisioner{
		simpleCfg:  simpleCfg,
		configPath: configPath,
		runner:     runner.NewCobraCommandRunner(nil, nil),
	}

	return prov
}

// Create provisions a k3d cluster using the native Cobra command.
func (k *K3dClusterProvisioner) Create(ctx context.Context, name string) error {
	args := k.appendConfigFlag(nil)
	args = k.appendImageFlag(args)

	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterCreate,
		args,
		name,
		"cluster create",
		func(target string) {
			if k.simpleCfg != nil {
				k.simpleCfg.Name = target
			}
		},
	)
}

// Delete removes a k3d cluster via the Cobra command.
func (k *K3dClusterProvisioner) Delete(ctx context.Context, name string) error {
	args := k.appendConfigFlag(nil)

	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterDelete,
		args,
		name,
		"cluster delete",
		nil,
	)
}

// Start resumes a stopped k3d cluster via Cobra.
func (k *K3dClusterProvisioner) Start(ctx context.Context, name string) error {
	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterStart,
		nil,
		name,
		"cluster start",
		nil,
	)
}

// Stop halts a running k3d cluster via Cobra.
func (k *K3dClusterProvisioner) Stop(ctx context.Context, name string) error {
	return k.runLifecycleCommand(
		ctx,
		clustercommand.NewCmdClusterStop,
		nil,
		name,
		"cluster stop",
		nil,
	)
}

// List returns cluster names reported by the Cobra command.
func (k *K3dClusterProvisioner) List(ctx context.Context) ([]string, error) {
	// Temporarily redirect logrus to discard output during list
	// to prevent JSON output from appearing in console
	originalLogOutput := logrus.StandardLogger().Out

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(originalLogOutput)

	// Lock to prevent concurrent modifications of os.Stdout
	listMutex.Lock()

	// Setup stdout redirection
	originalStdout := os.Stdout

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		listMutex.Unlock()

		return nil, fmt.Errorf("cluster list: create stdout pipe: %w", err)
	}

	os.Stdout = pipeWriter

	// Run the command
	output, runErr := k.runListCommand(ctx)

	// Close write end before reading and restore stdout while still holding the lock
	_ = pipeWriter.Close()
	os.Stdout = originalStdout

	// Unlock mutex before potentially blocking I/O operations
	listMutex.Unlock()

	// Discard any output that was written to our pipe
	copyErr := discardPipeOutput(pipeReader)
	_ = pipeReader.Close()

	if copyErr != nil {
		logrus.WithError(copyErr).Debug("failed to drain stdout pipe when listing k3d clusters")
	}

	if runErr != nil {
		return nil, fmt.Errorf("cluster list: %w", runErr)
	}

	return parseClusterNames(output)
}

// Exists returns whether the target cluster is present.
func (k *K3dClusterProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusters, err := k.List(ctx)
	if err != nil {
		return false, fmt.Errorf("list: %w", err)
	}

	target := k.resolveName(name)
	if target == "" {
		return false, nil
	}

	return slices.Contains(clusters, target), nil
}

// runListCommand executes the k3d cluster list command and returns the output.
func (k *K3dClusterProvisioner) runListCommand(ctx context.Context) (string, error) {
	cmd := clustercommand.NewCmdClusterList()
	args := []string{"--output", "json"}

	// Use a buffer runner to capture command output
	var buf bytes.Buffer

	listRunner := runner.NewCobraCommandRunner(&buf, io.Discard)

	res, runErr := listRunner.Run(ctx, cmd, args)
	if runErr != nil {
		return "", fmt.Errorf("run k3d cluster list: %w", runErr)
	}

	return strings.TrimSpace(res.Stdout), nil
}

// parseClusterNames parses JSON output and extracts cluster names.
func parseClusterNames(output string) ([]string, error) {
	if output == "" {
		return nil, nil
	}

	var entries []struct {
		Name string `json:"name"`
	}

	decodeErr := json.Unmarshal([]byte(output), &entries)
	if decodeErr != nil {
		return nil, fmt.Errorf("cluster list: parse output: %w", decodeErr)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name != "" {
			names = append(names, entry.Name)
		}
	}

	return names, nil
}

// discardPipeOutput reads and discards all data from a pipe reader.
func discardPipeOutput(pipeReader *os.File) error {
	_, err := io.Copy(io.Discard, pipeReader)
	if err != nil {
		return fmt.Errorf("copy pipe output: %w", err)
	}

	return nil
}

func (k *K3dClusterProvisioner) appendConfigFlag(args []string) []string {
	if k.configPath == "" {
		return args
	}

	return append(args, "--config", k.configPath)
}

// appendImageFlag adds the --image flag when no config file is used.
// This ensures the k3d CLI uses the image from our in-memory config
// instead of its internal default (which may be an older version).
func (k *K3dClusterProvisioner) appendImageFlag(args []string) []string {
	// Only add --image flag when no config file is used
	// When a config file exists, the image is read from the config file
	if k.configPath != "" {
		return args
	}

	// Get image from in-memory config, or use empty to let k3d decide
	if k.simpleCfg != nil && k.simpleCfg.Image != "" {
		return append(args, "--image", k.simpleCfg.Image)
	}

	return args
}

func (k *K3dClusterProvisioner) resolveName(name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}

	if k.simpleCfg != nil && strings.TrimSpace(k.simpleCfg.Name) != "" {
		return k.simpleCfg.Name
	}

	return ""
}

func (k *K3dClusterProvisioner) runLifecycleCommand(
	ctx context.Context,
	builder func() *cobra.Command,
	args []string,
	name string,
	errorPrefix string,
	onTarget func(string),
) error {
	cmd := builder()

	target := k.resolveName(name)
	if target != "" {
		args = append(args, target)
		if onTarget != nil {
			onTarget(target)
		}
	}

	_, runErr := k.runner.Run(ctx, cmd, args)
	if runErr != nil {
		return fmt.Errorf("%s: %w", errorPrefix, runErr)
	}

	return nil
}
