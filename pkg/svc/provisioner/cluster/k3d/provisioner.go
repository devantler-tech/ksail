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

	runner "github.com/devantler-tech/ksail/pkg/cmd/runner"
	clustercommand "github.com/k3d-io/k3d/v5/cmd/cluster"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
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
	// Configure logrus for k3d's console output
	// k3d uses logrus for logging, so we need to set it up properly
	logrus.SetOutput(os.Stdout)
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:      true,
		DisableTimestamp: false,
		FullTimestamp:    false,
		TimestampFormat:  "2006-01-02T15:04:05Z",
	})
	logrus.SetLevel(logrus.InfoLevel)

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

// stdoutMutex protects concurrent access to os.Stdout during List operations.
var stdoutMutex sync.Mutex

// List returns cluster names reported by the Cobra command.
func (k *K3dClusterProvisioner) List(ctx context.Context) ([]string, error) {
	// Temporarily redirect logrus to discard output during list
	// to prevent JSON output from appearing in console
	originalLogOutput := logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(originalLogOutput)

	// Lock to prevent concurrent modifications of os.Stdout
	stdoutMutex.Lock()
	defer stdoutMutex.Unlock()

	// Temporarily redirect os.Stdout to capture/suppress k3d's direct stdout writes
	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("cluster list: create stdout pipe: %w", err)
	}
	defer r.Close()
	os.Stdout = w
	defer func() {
		os.Stdout = originalStdout
		w.Close()
	}()

	cmd := clustercommand.NewCmdClusterList()
	args := []string{"--output", "json"}

	// Use a buffer runner to capture command output
	var buf bytes.Buffer
	listRunner := runner.NewCobraCommandRunner(&buf, io.Discard)

	res, runErr := listRunner.Run(ctx, cmd, args)

	// Close the write end before reading
	w.Close()

	// Discard any output that was written to our pipe
	if _, copyErr := io.Copy(io.Discard, r); copyErr != nil {
		logrus.WithError(copyErr).Debug("failed to drain stdout pipe when listing k3d clusters")
	}

	if runErr != nil {
		return nil, fmt.Errorf("cluster list: %w", runErr)
	}

	output := strings.TrimSpace(res.Stdout)
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

func (k *K3dClusterProvisioner) appendConfigFlag(args []string) []string {
	if k.configPath == "" {
		return args
	}

	return append(args, "--config", k.configPath)
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
