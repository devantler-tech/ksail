package cluster_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/rootcheck"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	dockerpkg "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/gkampitakis/go-snaps/snaps"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errSTSUnavailable       = errors.New("STS unavailable")
	errRequiredStateFailure = errors.New("required state write failed")
	errTTLCleanupFailure    = errors.New("TTL cleanup failed")
)

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EnabledCertManager_PrintsInstallStage(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"--cert-manager", "Enabled"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if !fake.called {
		t.Fatalf("expected cert-manager installer to be invoked")
	}

	// Normalize timing variance: keep --timing disabled in this test.
	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_DefaultCertManager_DoesNotInstall(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	factoryCalled := false

	restore := cluster.SetCertManagerInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			factoryCalled = true

			return &fakeInstaller{}, nil
		},
	)
	defer restore()

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if factoryCalled {
		t.Fatalf("expected cert-manager installer factory not to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

func setupGitOpsTestMocks(
	t *testing.T,
	engine v1alpha1.GitOpsEngine,
) (func() *fakeInstaller, *bool) {
	t.Helper()

	var fake *fakeInstaller

	ensureCalled := false

	// Override cluster provisioner factory to use fake provisioner
	t.Cleanup(cluster.SetProvisionerFactoryForTests(fakeFactory{}))

	// Set up the appropriate installer and ensure mocks based on the GitOps engine
	switch engine {
	case v1alpha1.GitOpsEngineArgoCD:
		setupArgoCDMocks(t, &fake, &ensureCalled)
	case v1alpha1.GitOpsEngineFlux:
		setupFluxMocks(t, &fake, &ensureCalled)
	case v1alpha1.GitOpsEngineNone:
		t.Fatalf("GitOpsEngineNone is not supported in this test helper")
	}

	// Mock registry service factory to avoid needing a real Docker client
	t.Cleanup(cluster.SetLocalRegistryServiceFactoryForTests(fakeRegistryServiceFactory))

	// Note: DockerClientInvoker is NOT overridden here - tests should call
	// setupMockRegistryBackend() before setupGitOpsTestMocks() to configure
	// a mock Docker client that will be used for all Docker operations.

	return func() *fakeInstaller { return fake }, &ensureCalled
}

func setupArgoCDMocks(t *testing.T, fake **fakeInstaller, ensureCalled *bool) {
	t.Helper()
	t.Cleanup(cluster.SetArgoCDInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			*fake = &fakeInstaller{}

			return *fake, nil
		},
	))
	t.Cleanup(cluster.SetEnsureArgoCDResourcesForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string) error {
			*ensureCalled = true

			return nil
		},
	))
	t.Cleanup(cluster.SetEnsureOCIArtifactForTests(
		func(_ context.Context, _ *cobra.Command, _ *v1alpha1.Cluster, _ string, _ io.Writer) (bool, error) {
			return true, nil
		},
	))
}

func setupFluxMocks(t *testing.T, fake **fakeInstaller, ensureCalled *bool) {
	t.Helper()
	t.Cleanup(cluster.SetFluxInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			*fake = &fakeInstaller{}

			return *fake, nil
		},
	))
	t.Cleanup(cluster.SetSetupFluxInstanceForTests(
		func(_ context.Context, _ string, _ *v1alpha1.Cluster, _ string, _ string) error {
			*ensureCalled = true

			return nil
		},
	))
	t.Cleanup(cluster.SetWaitForFluxReadyForTests(
		func(_ context.Context, _ string) error {
			return nil
		},
	))
	t.Cleanup(cluster.SetEnsureOCIArtifactForTests(
		func(_ context.Context, _ *cobra.Command, _ *v1alpha1.Cluster, _ string, _ io.Writer) (bool, error) {
			return true, nil
		},
	))
}

func TestCreate_GitOps_PrintsInstallStage(t *testing.T) {
	testCases := []struct {
		name   string
		engine v1alpha1.GitOpsEngine
		arg    string
	}{
		{name: "ArgoCD", engine: v1alpha1.GitOpsEngineArgoCD, arg: "ArgoCD"},
		{name: "Flux", engine: v1alpha1.GitOpsEngineFlux, arg: "Flux"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tmpRoot := t.TempDir()
			t.Setenv("TMPDIR", tmpRoot)

			workingDir := t.TempDir()
			t.Chdir(workingDir)
			writeTestConfigFiles(t, workingDir)
			setupMockRegistryBackend(t)

			fake, ensureCalled := setupGitOpsTestMocks(t, testCase.engine)

			cmd := cluster.NewCreateCmd()

			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetContext(context.Background())
			cmd.SetArgs([]string{"--gitops-engine", testCase.arg})

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
			}

			if !*ensureCalled {
				t.Fatalf("expected %s resources ensure hook to be invoked", testCase.name)
			}

			if installer := fake(); installer == nil || !installer.called {
				t.Fatalf("expected %s installer to be invoked", testCase.name)
			}

			snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
		})
	}
}

//nolint:paralleltest,funlen // end-to-end GitOps table mutates shared test hooks
func TestCreate_EKS_GitOps_UsesEksctlContext(t *testing.T) {
	testCases := []struct {
		name   string
		engine v1alpha1.GitOpsEngine
		arg    string
	}{
		{name: "ArgoCD", engine: v1alpha1.GitOpsEngineArgoCD, arg: "ArgoCD"},
		{name: "Flux", engine: v1alpha1.GitOpsEngineFlux, arg: "Flux"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			workingDir := t.TempDir()
			t.Chdir(workingDir)
			setupMockRegistryBackend(t)
			eksctlContext := writeEKSCreateTestFiles(t, workingDir)

			setupGitOpsTestMocks(t, testCase.engine)
			setEKSIdentityClient(t, &fakeEKSIdentityClient{
				accountID: "123456789012",
				cluster: immutableEKSClusterInRegion(
					"st-eks", "eu-west-1", immutableIdentityTime(),
				),
			})

			capturedContext := ""

			switch testCase.engine {
			case v1alpha1.GitOpsEngineArgoCD:
				t.Cleanup(cluster.SetEnsureArgoCDResourcesForTests(
					func(_ context.Context, _ string, cfg *v1alpha1.Cluster, _ string) error {
						capturedContext = cfg.Spec.Cluster.Connection.Context

						return nil
					},
				))
			case v1alpha1.GitOpsEngineFlux:
				t.Cleanup(cluster.SetSetupFluxInstanceForTests(
					func(_ context.Context, _ string, cfg *v1alpha1.Cluster, _, _ string) error {
						capturedContext = cfg.Spec.Cluster.Connection.Context

						return nil
					},
				))
			case v1alpha1.GitOpsEngineNone:
				t.Fatalf("GitOpsEngineNone is not supported in this test")
			}

			cmd := cluster.NewCreateCmd()
			cmd.SetContext(context.Background())
			cmd.SetArgs([]string{"--gitops-engine", testCase.arg})

			err := cmd.Execute()
			require.NoError(t, err)
			assert.Equal(t, eksctlContext, capturedContext)

			ownership, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
			require.NoError(t, loadErr)
			assert.Equal(t, "123456789012", ownership.AccountID)
			assert.Equal(t, immutableIdentityTime(), ownership.CreatedAt)
		})
	}
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_EKSCaptureFailureStopsPostCreateWorkflow(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupMockRegistryBackend(t)
	writeEKSCreateTestFiles(t, workingDir)
	require.NoError(t, state.DeleteClusterState("st-eks"))

	fake, ensureCalled := setupGitOpsTestMocks(t, v1alpha1.GitOpsEngineArgoCD)
	setEKSIdentityClient(t, &fakeEKSIdentityClient{
		accountErr: errSTSUnavailable,
	})

	cmd := cluster.NewCreateCmd()

	var output bytes.Buffer

	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--gitops-engine", "ArgoCD"})

	err := cmd.Execute()
	require.ErrorContains(t, err, "capture immutable EKS ownership identity")
	assert.Contains(t, output.String(), "cluster created")
	assert.False(t, *ensureCalled)
	assert.Nil(t, fake())

	_, loadErr := state.LoadEKSOwnershipState("st-eks", "eu-west-1")
	require.ErrorIs(t, loadErr, state.ErrEKSOwnershipStateNotFound)
}

func TestFinishCreateWithTTL_CleansUpWithoutWaitingAfterPersistenceFailure(t *testing.T) {
	t.Parallel()

	cleanupCalled := false
	waitCalled := false

	err := cluster.ExportFinishCreateWithTTL(
		errRequiredStateFailure,
		func() error {
			cleanupCalled = true

			return errTTLCleanupFailure
		},
		func() error {
			waitCalled = true

			return nil
		},
	)

	assert.True(
		t,
		cleanupCalled,
		"a TTL cluster must be deleted after required state persistence fails",
	)
	assert.False(t, waitCalled, "a known-fatal state error must not enter the normal TTL wait")
	require.ErrorIs(t, err, errRequiredStateFailure)
	require.ErrorIs(t, err, errTTLCleanupFailure)
}

//nolint:paralleltest // mutates process environment, working directory, and shared hooks.
func TestCreate_EKSProfileRegionFeedsIdentityCapture(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	setupMockRegistryBackend(t)
	writeEKSProfileRegionCreateTestFiles(t, workingDir)
	configureEKSProfileRegionCredentials(t, workingDir)

	setupGitOpsTestMocks(t, v1alpha1.GitOpsEngineArgoCD)

	var capturedRegion string

	setEKSIdentityClientWithResolutionObserver(
		t,
		&fakeEKSIdentityClient{
			accountID: "123456789012",
			cluster: immutableEKSClusterInRegion(
				"st-eks", "eu-north-1", immutableIdentityTime(),
			),
		},
		func(region string, _ credentials.AWSResolution) {
			capturedRegion = region
		},
	)

	cmd := cluster.NewCreateCmd()
	cmd.SetContext(t.Context())
	cmd.SetArgs([]string{"--gitops-engine", "ArgoCD"})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, "eu-north-1", capturedRegion)

	ownership, err := state.LoadEKSOwnershipState("st-eks", "eu-north-1")
	require.NoError(t, err)
	//nolint:gosec // G101: these are environment-variable names, never credential values.
	assert.Equal(t, v1alpha1.OptionsAWS{
		ProfileEnvVar:         "KSAIL_PROFILE",
		RegionEnvVar:          "AWS_REGION",
		AccessKeyIDEnvVar:     "AWS_ACCESS_KEY_ID",
		SecretAccessKeyEnvVar: "AWS_SECRET_ACCESS_KEY",
		SessionTokenEnvVar:    "AWS_SESSION_TOKEN",
	}, ownership.AWSOptions)
}

func writeEKSProfileRegionCreateTestFiles(t *testing.T, workingDir string) {
	t.Helper()

	writeFile(t, workingDir, "ksail.yaml", `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: st-eks
spec:
  cluster:
    distribution: EKS
    provider: AWS
    distributionConfig: eks.yaml
    metricsServer: Disabled
    localRegistry:
      registry: ""
    connection:
      kubeconfig: ./kubeconfig
  provider:
    aws:
      profileEnvVar: KSAIL_PROFILE
`)
	writeFile(t, workingDir, "eks.yaml", `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: st-eks
`)
	writeFile(t, workingDir, "kubeconfig", `apiVersion: v1
kind: Config
current-context: arn:aws:iam::123456789012:role/ci@st-eks.eu-north-1.eksctl.io
clusters:
  - cluster:
      server: https://example.invalid
    name: arn:aws:iam::123456789012:role/ci@st-eks.eu-north-1.eksctl.io
contexts:
  - context:
      cluster: arn:aws:iam::123456789012:role/ci@st-eks.eu-north-1.eksctl.io
      user: arn:aws:iam::123456789012:role/ci@st-eks.eu-north-1.eksctl.io
    name: arn:aws:iam::123456789012:role/ci@st-eks.eu-north-1.eksctl.io
users:
  - name: arn:aws:iam::123456789012:role/ci@st-eks.eu-north-1.eksctl.io
    user:
      token: fake
`)
}

func configureEKSProfileRegionCredentials(t *testing.T, workingDir string) {
	t.Helper()

	credentialsFile := filepath.Join(workingDir, "aws-credentials")
	configFile := filepath.Join(workingDir, "aws-config")
	writeFile(t, workingDir, "aws-credentials", `[selected-profile]
aws_access_key_id = profile-access
aws_secret_access_key = profile-secret
`)
	writeFile(t, workingDir, "aws-config", `[profile selected-profile]
region = eu-north-1
`)
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("KSAIL_PROFILE", "selected-profile")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile)
	t.Setenv("AWS_CONFIG_FILE", configFile)
}

func writeEKSCreateTestFiles(t *testing.T, workingDir string) string {
	t.Helper()

	const eksctlContext = "arn:aws:iam::123456789012:role/ci@st-eks.eu-west-1.eksctl.io"

	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-secret")
	t.Setenv("AWS_SESSION_TOKEN", "fixture-session")

	writeFile(t, workingDir, "ksail.yaml", `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: st-eks
spec:
  cluster:
    distribution: EKS
    provider: AWS
    distributionConfig: eks.yaml
    metricsServer: Disabled
    localRegistry:
      registry: ""
    connection:
      kubeconfig: ./kubeconfig
`)
	writeFile(t, workingDir, "eks.yaml", `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: st-eks
  region: eu-west-1
`)
	writeFile(t, workingDir, "kubeconfig", `apiVersion: v1
kind: Config
current-context: `+eksctlContext+`
clusters:
  - cluster:
      server: https://example.invalid
    name: `+eksctlContext+`
contexts:
  - context:
      cluster: `+eksctlContext+`
      user: `+eksctlContext+`
    name: `+eksctlContext+`
users:
  - name: `+eksctlContext+`
    user:
      token: fake
`)

	return eksctlContext
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_CSIEnabled_InstallsOnKind(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    csi: Enabled
    metricsServer: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	writeFile(t, workingDir, "ksail.yaml", ksailYAML)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	fake := &fakeInstaller{}

	restore := cluster.SetCSIInstallerFactoryForTests(
		func(_ *v1alpha1.Cluster) (installer.Installer, error) {
			return fake, nil
		},
	)
	defer restore()

	setupMockRegistryBackend(t)

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	if !fake.called {
		t.Fatalf("expected CSI installer to be invoked")
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_DefaultCSI_DoesNotInstall(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir)
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_Minimal_PrintsOnlyClusterLifecycle tests cluster creation with no extras.
// This verifies the minimal output when all optional components are disabled.
// Uses config with localRegistry disabled to skip registry stages.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_Minimal_PrintsOnlyClusterLifecycle(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)
	writeTestConfigFiles(t, workingDir) // Uses config with localRegistry disabled
	setupMockRegistryBackend(t)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

// TestCreate_NoConfigFile_FlagsOnly verifies that cluster creation succeeds
// when no ksail.yaml is present, relying entirely on CLI flags for configuration.
// This covers the init=false code path exercised by E2E system tests.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_NoConfigFile_FlagsOnly(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	// Write only the distribution config and kubeconfig — no ksail.yaml.
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test-no-config\nnodes: []\n",
	)
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\ncurrent-context: kind-test-no-config\nclusters:\n"+
			"- cluster:\n    server: https://127.0.0.1:6443\n  name: kind-test-no-config\n"+
			"contexts:\n- context:\n    cluster: kind-test-no-config\n"+
			"    user: kind-test-no-config\n  name: kind-test-no-config\n"+
			"users:\n- name: kind-test-no-config\n  user:\n    token: fake\n",
	)

	setupMockRegistryBackend(t)

	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{
		"--distribution", "Vanilla",
		"--name", "test-no-config",
		"--kubeconfig", "./kubeconfig",
	})

	err := cmd.Execute()
	require.NoError(
		t,
		err,
		"create command should succeed without ksail.yaml; output:\n%s",
		out.String(),
	)

	output := out.String()
	// The cluster lifecycle stages should have executed.
	require.Contains(t, output, "Create cluster", "output should contain cluster lifecycle text")
}

// TestCreate_NonEKSNameOverrideReachesProvisioner guards the shared mutation path: rejecting EKS
// source-name drift must not suppress explicit --name overrides for other distributions.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_NonEKSNameOverrideReachesProvisioner(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: source-kind-name\nnodes: []\n",
	)
	writeFile(t, workingDir, "kubeconfig", "apiVersion: v1\nkind: Config\n")
	setupMockRegistryBackend(t)

	var createdName string

	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{createName: &createdName})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(t.Context())
	cmd.SetArgs([]string{
		"--distribution", "Vanilla",
		"--name", "explicit-kind-name",
		"--kubeconfig", "./kubeconfig",
	})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "explicit-kind-name", createdName)
}

// TestCreateRejectsWhitespaceOnlyName preserves explicit-flag validation: whitespace is not an
// absent override and must not silently fall back to the distribution config target.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreateRejectsWhitespaceOnlyName(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: source-kind-name\nnodes: []\n",
	)
	writeFile(t, workingDir, "kubeconfig", "apiVersion: v1\nkind: Config\n")
	setupMockRegistryBackend(t)

	var createdName string

	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{createName: &createdName})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetContext(t.Context())
	cmd.SetArgs([]string{
		"--distribution", "Vanilla",
		"--name", "   ",
		"--kubeconfig", "./kubeconfig",
	})

	err := cmd.Execute()
	require.ErrorContains(t, err, "invalid cluster name")
	assert.Empty(t, createdName)
}

// TestCreate_NoConfigFile_WithComponentFlags verifies that component flags
// (e.g. --metrics-server Disabled) are respected when no ksail.yaml exists.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_NoConfigFile_WithComponentFlags(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	// Write only the distribution config and kubeconfig — no ksail.yaml.
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test-flags\nnodes: []\n",
	)
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\ncurrent-context: kind-test-flags\nclusters:\n"+
			"- cluster:\n    server: https://127.0.0.1:6443\n  name: kind-test-flags\n"+
			"contexts:\n- context:\n    cluster: kind-test-flags\n    user: kind-test-flags\n  name: kind-test-flags\n"+
			"users:\n- name: kind-test-flags\n  user:\n    token: fake\n",
	)

	setupMockRegistryBackend(t)

	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{
		"--distribution", "Vanilla",
		"--name", "test-flags",
		"--metrics-server", "Disabled",
		"--kubeconfig", "./kubeconfig",
	})

	err := cmd.Execute()
	require.NoError(
		t,
		err,
		"create command should succeed without ksail.yaml; output:\n%s",
		out.String(),
	)

	output := out.String()
	// Metrics-server was disabled, so it should not appear in the output.
	require.NotContains(t, output, "metrics-server",
		"output should not mention metrics-server install when it is disabled")
}

// TestCreate_LocalRegistryDisabled_SkipsRegistryStages tests cluster creation with local registry disabled.
//
//nolint:paralleltest // uses t.Chdir and mutates shared test hooks
func TestCreate_LocalRegistryDisabled_SkipsRegistryStages(t *testing.T) {
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	ksailYAML := `apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Vanilla
    distributionConfig: kind.yaml
    localRegistry:
      enabled: false
    metricsServer: Disabled
    connection:
      kubeconfig: ./kubeconfig
`
	writeFile(t, workingDir, "ksail.yaml", ksailYAML)
	writeFile(
		t,
		workingDir,
		"kind.yaml",
		"kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nname: test\nnodes: []\n",
	)
	writeFile(
		t,
		workingDir,
		"kubeconfig",
		"apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\n",
	)

	// Override cluster provisioner factory to use fake provisioner
	restoreFactory := cluster.SetProvisionerFactoryForTests(fakeFactory{})
	defer restoreFactory()

	setupMockRegistryBackend(t)

	cmd := cluster.NewCreateCmd()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("create command failed: %v\noutput:\n%s", err, out.String())
	}

	snaps.MatchSnapshot(t, trimTrailingNewline(out.String()))
}

func TestShouldPushOCIArtifact_FluxWithLocalRegistry(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.True(t, result, "Should push when Flux is enabled with local registry")
}

func TestShouldPushOCIArtifact_ArgoCDWithLocalRegistry(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineArgoCD,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.True(t, result, "Should push when ArgoCD is enabled with local registry")
}

func TestShouldPushOCIArtifact_NoLocalRegistryShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine:  v1alpha1.GitOpsEngineFlux,
				LocalRegistry: v1alpha1.LocalRegistry{
					// Empty registry - disabled
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.False(t, result, "Should not push when local registry is disabled")
}

func TestShouldPushOCIArtifact_NoGitOpsEngineShouldNotPush(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				GitOpsEngine: v1alpha1.GitOpsEngineNone,
				LocalRegistry: v1alpha1.LocalRegistry{
					Registry: "localhost:5000",
				},
			},
		},
	}

	result := cluster.ExportShouldPushOCIArtifact(clusterCfg)
	require.False(t, result, "Should not push when GitOps engine is none")
}

func TestSetupK3dCSI_DisablesCSI(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Verify the flag was added
	found := false

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=local-storage" {
			found = true

			require.Equal(t, []string{"server:*"}, arg.NodeFilters)

			break
		}
	}

	require.True(t, found, "--disable=local-storage flag should be added")
}

func TestSetupK3dCSI_DoesNotDuplicateFlag(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{
		Options: v1alpha5.SimpleConfigOptions{
			K3sOptions: v1alpha5.SimpleConfigOptionsK3s{
				ExtraArgs: []v1alpha5.K3sArgWithNodeFilters{
					{
						Arg:         "--disable=local-storage",
						NodeFilters: []string{"server:*"},
					},
				},
			},
		},
	}

	cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Count occurrences of the flag
	count := 0

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == "--disable=local-storage" {
			count++
		}
	}

	require.Equal(t, 1, count, "flag should not be duplicated")
}

func TestSetupK3dCSI_DoesNothingForNonK3s(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				CSI:          v1alpha1.CSIDisabled,
			},
		},
	}

	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

	// Verify no flags were added
	require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
}

func TestSetupK3dCSI_DoesNothingWhenCSINotDisabled(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		csi  v1alpha1.CSI
	}{
		{"default", v1alpha1.CSIDefault},
		{"enabled", v1alpha1.CSIEnabled},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clusterCfg := &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						Distribution: v1alpha1.DistributionK3s,
						CSI:          testCase.csi,
					},
				},
			}

			k3dConfig := &v1alpha5.SimpleConfig{}

			cluster.ExportSetupK3dCSI(clusterCfg, k3dConfig)

			// Verify no flags were added
			require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
		})
	}
}

func TestSetupK3dCNI_CiliumDisablesFlannelNetworkPolicyAndTraefik(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	args := k3dConfig.Options.K3sOptions.ExtraArgs
	require.Len(
		t,
		args,
		3,
		"Cilium should add flannel-backend=none, disable-network-policy, and disable=traefik",
	)

	argMap := make(map[string]v1alpha5.K3sArgWithNodeFilters, len(args))
	for _, a := range args {
		argMap[a.Arg] = a
	}

	for _, flag := range []string{"--flannel-backend=none", "--disable-network-policy", disableTraefikArg} {
		a, ok := argMap[flag]
		require.Truef(t, ok, "%q should be present", flag)
		require.Equalf(
			t,
			[]string{"server:*"},
			a.NodeFilters,
			"node filters for %q should be [\"server:*\"]",
			flag,
		)
	}
}

func TestSetupK3dCNI_CiliumDisablesTraefik(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	found := false

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == disableTraefikArg {
			found = true

			require.Equal(t, []string{"server:*"}, arg.NodeFilters)

			break
		}
	}

	require.True(t, found, "--disable=traefik should be added when using Cilium CNI")
}

func TestSetupK3dCNI_CalicoDoesNotDisableTraefik(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICalico,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		require.NotEqual(
			t,
			disableTraefikArg,
			arg.Arg,
			"--disable=traefik should NOT be added for Calico",
		)
	}
}

func TestSetupK3dCNI_DoesNotDuplicateTraefikFlag(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{
		Options: v1alpha5.SimpleConfigOptions{
			K3sOptions: v1alpha5.SimpleConfigOptionsK3s{
				ExtraArgs: []v1alpha5.K3sArgWithNodeFilters{
					{Arg: disableTraefikArg, NodeFilters: []string{"server:*"}},
				},
			},
		},
	}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	count := 0

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == disableTraefikArg {
			count++
		}
	}

	require.Equal(t, 1, count, "--disable=traefik should not be duplicated")
}

func TestSetupK3dCNI_AgentScopedTraefikFlagDoesNotPreventServerScopedAdd(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{
		Options: v1alpha5.SimpleConfigOptions{
			K3sOptions: v1alpha5.SimpleConfigOptionsK3s{
				ExtraArgs: []v1alpha5.K3sArgWithNodeFilters{
					{Arg: disableTraefikArg, NodeFilters: []string{"agent:*"}},
				},
			},
		},
	}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	serverDisabled := false

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == disableTraefikArg {
			for _, f := range arg.NodeFilters {
				if f == "server:*" {
					serverDisabled = true
				}
			}
		}
	}

	require.True(
		t,
		serverDisabled,
		"--disable=traefik with server:* should be added even when an agent:* entry exists",
	)
}

func TestSetupK3dCNI_DoesNothingForDefaultCNI(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionK3s,
				CNI:          v1alpha1.CNIDefault,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
}

func TestSetupK3dCNI_DoesNothingForNonK3s(t *testing.T) {
	t.Parallel()

	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: v1alpha1.DistributionVanilla,
				CNI:          v1alpha1.CNICilium,
			},
		},
	}
	k3dConfig := &v1alpha5.SimpleConfig{}

	cluster.ExportSetupK3dCNI(clusterCfg, k3dConfig)

	require.Empty(t, k3dConfig.Options.K3sOptions.ExtraArgs)
}

// TestEnsureLocalRegistriesReady_CloudProviders verifies that Docker infrastructure
// (local registry container, mirror registry containers, Docker network) is skipped
// for cloud providers (Omni, Hetzner). Cloud providers run nodes on remote servers
// and cannot access local Docker infrastructure.
func TestEnsureLocalRegistriesReady_CloudProviders(t *testing.T) {
	t.Parallel()

	cloudProviders := []v1alpha1.Provider{
		v1alpha1.ProviderOmni,
		v1alpha1.ProviderHetzner,
	}

	for _, provider := range cloudProviders {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()

			// Set up a docker invoker that errors on any call.
			// If Docker infra stages are incorrectly executed for cloud providers,
			// this invoker will cause the test to fail.
			errDockerInvoker := func(_ *cobra.Command, _ func(dockerpkg.Client) error) error {
				t.Errorf("Docker should not be called for cloud provider %s", provider)

				return nil
			}

			localDeps := localregistry.NewDependencies(
				localregistry.WithDockerInvoker(errDockerInvoker),
			)

			cmd := &cobra.Command{Use: "test"}
			cmd.Flags().StringSlice("mirror-registry", []string{}, "")
			cmd.SetContext(context.Background())

			ctx := &localregistry.Context{
				ClusterCfg: &v1alpha1.Cluster{
					Spec: v1alpha1.Spec{
						Cluster: v1alpha1.ClusterSpec{
							Distribution: v1alpha1.DistributionTalos,
							Provider:     provider,
						},
					},
				},
			}

			v := viper.New()
			cfgManager := &ksailconfigmanager.ConfigManager{Viper: v}

			deps := lifecycle.Deps{Timer: timer.New()}

			err := cluster.ExportEnsureLocalRegistriesReady(
				cmd,
				ctx,
				deps,
				cfgManager,
				localDeps,
			)
			require.NoError(
				t,
				err,
				"ensureLocalRegistriesReady should succeed for cloud provider %s without Docker",
				provider,
			)
		})
	}
}

// mockKubeconfigRefresher is a test double for clusterprovisioner.KubeconfigRefresher.
type mockKubeconfigRefresher struct {
	err    error
	called bool
	onCall func() // optional side-effect (e.g., create the kubeconfig file)
}

func (m *mockKubeconfigRefresher) RefreshKubeconfig(_ context.Context, _ string) error {
	m.called = true
	if m.onCall != nil {
		m.onCall()
	}

	return m.err
}

// newTestCmd returns a minimal *cobra.Command suitable for unit tests.
func newTestCmd(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	return cmd
}

// newTestClusterCfg returns a *v1alpha1.Cluster whose kubeconfig path is set to
// the given absolute path with a fixed test context name.
func newTestClusterCfg(kubeconfigPath string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Cluster.Connection.Kubeconfig = kubeconfigPath
	cfg.Spec.Cluster.Connection.Context = "test-context"

	return cfg
}

// TestRefreshAndVerifyKubeconfig_ValidKubeconfigSkipsRefresh verifies that when a
// valid kubeconfig already exists (staleness check returns false), the refresh is
// skipped entirely and no error is returned.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_ValidKubeconfigSkipsRefresh(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(kcPath, []byte("placeholder"), 0o600))

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return false })
	defer restore()

	refresher := &mockKubeconfigRefresher{}
	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.NoError(t, err)
	assert.False(t, refresher.called, "refresher should not be called when kubeconfig is valid")
}

// TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshSucceeds verifies that when no
// kubeconfig exists and the refresh succeeds (and creates the file), no error is returned.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshSucceeds(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	// File does not exist yet.

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{
		onCall: func() {
			// Simulate the provisioner writing the kubeconfig file.
			_ = os.WriteFile(kcPath, []byte("placeholder"), 0o600)
		},
	}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.NoError(t, err)
	assert.True(t, refresher.called)
}

// TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshFails verifies that when no
// kubeconfig exists and the refresh also fails, a hard error is returned.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_NoKubeconfigRefreshFails(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	// File does not exist.

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{err: errClusterPureTalosConfigEmpty}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to refresh kubeconfig")
	require.ErrorContains(t, err, "talos config file is empty")
	assert.True(t, refresher.called)
}

// TestRefreshAndVerifyKubeconfig_StaleKubeconfigRefreshFailsWarns verifies that when
// a stale kubeconfig already exists and the refresh fails, the function warns but
// returns nil so that downstream operations can still attempt to use the existing file.
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_StaleKubeconfigRefreshFailsWarns(t *testing.T) {
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(kcPath, []byte("placeholder"), 0o600))

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{err: errClusterPureTalosConfigEmpty}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.NoError(t, err, "should warn and proceed when stale file exists but refresh fails")
	assert.True(t, refresher.called)
}

// TestRefreshAndVerifyKubeconfig_StatPermissionError verifies that non-ENOENT
// os.Stat errors (e.g. permission denied) are returned immediately rather than
// being misinterpreted as "file missing".
//
//nolint:paralleltest // Mutates the global isKubeconfigStaleFunc.
func TestRefreshAndVerifyKubeconfig_StatPermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if rootcheck.IsRootUser() {
		t.Skip("running as root — permission checks are bypassed")
	}

	dir := t.TempDir()
	// Create a directory that we can't read (os.Stat on a file inside it will
	// fail with EACCES on most Unix systems).
	noAccessDir := filepath.Join(dir, "noaccess")
	require.NoError(t, os.MkdirAll(noAccessDir, 0o000))

	t.Cleanup(
		func() { _ = os.Chmod(noAccessDir, 0o750) }, //nolint:gosec // Restore access for cleanup.
	)

	kcPath := filepath.Join(noAccessDir, "config")

	restore := cluster.ExportSetIsKubeconfigStaleFunc(func(_, _ string) bool { return true })
	defer restore()

	refresher := &mockKubeconfigRefresher{}

	err := cluster.ExportRefreshAndVerifyKubeconfig(
		newTestCmd(t),
		refresher,
		newTestClusterCfg(kcPath),
		"test-cluster",
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to stat kubeconfig")
	assert.False(t, refresher.called, "should not attempt refresh on permission error")
}

func TestResolveCreatedContextName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		clusterName  string
		expected     string
	}{
		{
			name:         "k3s on kubernetes provider uses k3k prefix",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderKubernetes,
			clusterName:  "nested-k3s",
			expected:     "k3k-nested-k3s",
		},
		{
			name:         "k3s on docker provider uses standalone k3d prefix",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderDocker,
			clusterName:  "my-cluster",
			expected:     "k3d-my-cluster",
		},
		{
			name:         "vanilla on kubernetes provider uses standalone kind prefix",
			distribution: v1alpha1.DistributionVanilla,
			provider:     v1alpha1.ProviderKubernetes,
			clusterName:  "nested-vanilla",
			expected:     "kind-nested-vanilla",
		},
		{
			name:         "empty cluster name returns empty",
			distribution: v1alpha1.DistributionK3s,
			provider:     v1alpha1.ProviderKubernetes,
			clusterName:  "",
			expected:     "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := cluster.ExportResolveCreatedContextName(
				testCase.distribution,
				testCase.provider,
				testCase.clusterName,
			)
			assert.Equal(t, testCase.expected, got)
		})
	}
}
