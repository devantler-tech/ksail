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

	runner "github.com/devantler-tech/ksail/v5/pkg/runner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/detector"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
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
	simpleCfg         *v1alpha5.SimpleConfig
	configPath        string
	runner            runner.CommandRunner
	componentDetector *detector.ComponentDetector
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
// Returns clustererrors.ErrClusterNotFound if the cluster does not exist.
func (k *K3dClusterProvisioner) Delete(ctx context.Context, name string) error {
	// Check if cluster exists before attempting to delete
	target := k.resolveName(name)

	exists, err := k.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %s", clustererrors.ErrClusterNotFound, target)
	}

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
	// to prevent log messages from appearing in console
	originalLogOutput := logrus.StandardLogger().Out

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(originalLogOutput)

	// Lock to prevent concurrent modifications of os.Stdout
	listMutex.Lock()

	// Setup stdout redirection - k3d's PrintClusters writes directly to os.Stdout
	// using fmt.Println, not through Cobra's cmd.OutOrStdout(), so we must
	// capture from os.Stdout directly.
	originalStdout := os.Stdout

	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		listMutex.Unlock()

		return nil, fmt.Errorf("cluster list: create stdout pipe: %w", err)
	}

	os.Stdout = pipeWriter

	// Run the command in a goroutine since we need to read from the pipe
	// while the command is running (otherwise it may block on a full pipe buffer)
	errChan := make(chan error, 1)

	go func() {
		_, runErr := k.runListCommand(ctx)
		// Close write end to signal EOF to the reader
		_ = pipeWriter.Close()

		errChan <- runErr
	}()

	// Read all output from the pipe (this is the JSON from k3d)
	var outputBuf bytes.Buffer

	_, copyErr := io.Copy(&outputBuf, pipeReader)
	_ = pipeReader.Close()

	// Restore stdout while still holding the lock
	os.Stdout = originalStdout

	// Unlock mutex after restoring stdout
	listMutex.Unlock()

	// Wait for command to complete and get any error
	runErr := <-errChan

	if copyErr != nil {
		return nil, fmt.Errorf("cluster list: read stdout pipe: %w", copyErr)
	}

	if runErr != nil {
		return nil, fmt.Errorf("cluster list: %w", runErr)
	}

	return parseClusterNames(strings.TrimSpace(outputBuf.String()))
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

// WithComponentDetector sets the component detector for querying cluster state.
func (k *K3dClusterProvisioner) WithComponentDetector(d *detector.ComponentDetector) {
	k.componentDetector = d
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
