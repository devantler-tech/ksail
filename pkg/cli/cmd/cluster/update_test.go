package cluster

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
)

func TestNewUpdateCmd(t *testing.T) {
	t.Parallel()

	runtimeContainer := &runtime.Runtime{}
	cmd := NewUpdateCmd(runtimeContainer)

	// Verify command basics
	if cmd.Use != "update" {
		t.Errorf("expected Use to be 'update', got %q", cmd.Use)
	}

	if cmd.Short == "" {
		t.Error("expected Short description to be set")
	}

	if cmd.Long == "" {
		t.Error("expected Long description to be set")
	}

	// Verify flags
	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Error("expected --force flag to exist")
	}

	nameFlag := cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("expected --name flag to exist")
	}

	mirrorRegistryFlag := cmd.Flags().Lookup("mirror-registry")
	if mirrorRegistryFlag == nil {
		t.Error("expected --mirror-registry flag to exist")
	}
}

func TestPromptForUpdateConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "user confirms with 'yes'",
			input:    "yes\n",
			expected: true,
		},
		{
			name:     "user confirms with 'YES'",
			input:    "YES\n",
			expected: true,
		},
		{
			name:     "user confirms with 'Yes'",
			input:    "Yes\n",
			expected: true,
		},
		{
			name:     "user rejects with 'no'",
			input:    "no\n",
			expected: false,
		},
		{
			name:     "user rejects with empty input",
			input:    "\n",
			expected: false,
		},
		{
			name:     "user rejects with random text",
			input:    "maybe\n",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runtimeContainer := &runtime.Runtime{}
			cmd := NewUpdateCmd(runtimeContainer)

			// Set up input/output buffers
			inputBuf := bytes.NewBufferString(tt.input)
			outputBuf := &bytes.Buffer{}

			cmd.SetIn(inputBuf)
			cmd.SetOut(outputBuf)
			cmd.SetErr(outputBuf)

			// Test prompt function
			result := promptForUpdateConfirmation(cmd, "test-cluster")

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// MockProvisioner implements a test provisioner for update command testing.
type MockUpdateProvisioner struct {
	ExistsFunc func(ctx context.Context, name string) (bool, error)
	CreateFunc func(ctx context.Context, name string) error
	DeleteFunc func(ctx context.Context, name string) error
}

func (m *MockUpdateProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	if m.ExistsFunc != nil {
		return m.ExistsFunc(ctx, name)
	}

	return false, nil
}

func (m *MockUpdateProvisioner) Create(ctx context.Context, name string) error {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, name)
	}

	return nil
}

func (m *MockUpdateProvisioner) Delete(ctx context.Context, name string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, name)
	}

	return nil
}

func (m *MockUpdateProvisioner) Start(_ context.Context, _ string) error {
	return nil
}

func (m *MockUpdateProvisioner) Stop(_ context.Context, _ string) error {
	return nil
}

func (m *MockUpdateProvisioner) List(_ context.Context) ([]clusterprovisioner.ClusterInfo, error) {
	return nil, nil
}

func (m *MockUpdateProvisioner) ExportKubeconfig(_ context.Context, _, _ string) error {
	return nil
}

func (m *MockUpdateProvisioner) GetKubeconfig(_ context.Context, _ string) (string, error) {
	return "", nil
}

// MockUpdateFactory implements a test factory for update command testing.
type MockUpdateFactory struct {
	CreateFunc func(dist v1alpha1.Distribution, prov v1alpha1.Provider) (clusterprovisioner.ClusterProvisioner, error)
}

func (m *MockUpdateFactory) Create(dist v1alpha1.Distribution, prov v1alpha1.Provider) (clusterprovisioner.ClusterProvisioner, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(dist, prov)
	}

	return &MockUpdateProvisioner{}, nil
}

func TestHandleUpdateRunE_ClusterDoesNotExist(t *testing.T) {
	t.Parallel()

	// Create a mock provisioner that returns false for Exists
	mockProvisioner := &MockUpdateProvisioner{
		ExistsFunc: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
	}

	mockFactory := &MockUpdateFactory{
		CreateFunc: func(_ v1alpha1.Distribution, _ v1alpha1.Provider) (clusterprovisioner.ClusterProvisioner, error) {
			return mockProvisioner, nil
		},
	}

	// Inject the mock factory
	setClusterProvisionerFactoryForTesting(mockFactory)

	defer func() {
		setClusterProvisionerFactoryForTesting(nil)
	}()

	// Create command and config manager
	runtimeContainer := &runtime.Runtime{}
	cmd := NewUpdateCmd(runtimeContainer)

	// Set up test configuration file
	testConfig := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
    cni: Cilium
`

	cfgPath := t.TempDir() + "/ksail.yaml"

	err := ksailconfigmanager.WriteClusterConfigToFile(
		&v1alpha1.Cluster{
			TypeMeta: v1alpha1.NewTypeMeta(),
			Spec: v1alpha1.Spec{
				Cluster: v1alpha1.ClusterSpec{
					Distribution: v1alpha1.DistributionVanilla,
					Provider:     v1alpha1.ProviderDocker,
					CNI:          v1alpha1.CNICilium,
				},
			},
		},
		cfgPath,
	)
	if err != nil {
		t.Fatalf("failed to create test config: %v", err)
	}

	// Set up buffers
	inputBuf := bytes.NewBufferString("yes\n")
	outputBuf := &bytes.Buffer{}

	cmd.SetIn(inputBuf)
	cmd.SetOut(outputBuf)
	cmd.SetErr(outputBuf)

	// Set working directory to temp dir
	cmd.Flags().Set("config", cfgPath)

	// Execute command
	err = cmd.Execute()

	// Should fail because cluster doesn't exist
	if err == nil {
		t.Fatal("expected error when cluster doesn't exist, got nil")
	}

	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected error message to contain 'does not exist', got: %v", err)
	}
}
