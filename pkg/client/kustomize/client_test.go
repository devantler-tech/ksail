package kustomize_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/gkampitakis/go-snaps/snaps"
)

const simpleConfigMapKustomization = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - configmap.yaml
`

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	client := kustomize.NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestBuild_ValidKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple ConfigMap manifest
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`

	err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(configMapYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write configmap: %v", err)
	}

	// Create a kustomization.yaml
	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(simpleConfigMapKustomization),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify output using snapshot
	snaps.MatchSnapshot(t, output.String())
}

func TestBuild_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	client := kustomize.NewClient()
	ctx := context.Background()

	_, err := client.Build(ctx, "/nonexistent/directory")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestBuild_MissingKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory without kustomization.yaml
	tmpDir := t.TempDir()

	client := kustomize.NewClient()
	ctx := context.Background()

	_, err := client.Build(ctx, tmpDir)
	if err == nil {
		t.Fatal("expected error for missing kustomization.yaml")
	}
}

func TestBuild_InvalidKustomization(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with invalid kustomization
	tmpDir := t.TempDir()

	// Create an invalid kustomization.yaml (reference to non-existent file)
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - nonexistent.yaml
`

	err := os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	_, err = client.Build(ctx, tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid kustomization")
	}
}

func setupComplexKustomization(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create base resources
	namespaceYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

	err := os.WriteFile(filepath.Join(tmpDir, "namespace.yaml"), []byte(namespaceYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write namespace: %v", err)
	}

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: test-namespace
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test
        image: nginx:latest
`

	err = os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deploymentYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write deployment: %v", err)
	}

	// Create a kustomization with commonLabels
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - namespace.yaml
  - deployment.yaml
commonLabels:
  environment: test
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	return tmpDir
}

func TestBuild_ComplexKustomization(t *testing.T) {
	t.Parallel()

	tmpDir := setupComplexKustomization(t)

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify output using snapshot
	snaps.MatchSnapshot(t, output.String())
}

func TestBuild_OutputIsValidYAML(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple resource
	serviceYAML := `apiVersion: v1
kind: Service
metadata:
  name: test-service
  namespace: default
spec:
  selector:
    app: test
  ports:
  - port: 80
    targetPort: 8080
`

	err := os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(serviceYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write service: %v", err)
	}

	// Create a kustomization.yaml
	kustomizationYAML := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - service.yaml
`

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(kustomizationYAML),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify output is not empty
	if output.Len() == 0 {
		t.Fatal("expected non-empty output")
	}

	// Verify output using snapshot
	snaps.MatchSnapshot(t, output.String())
}

func TestBuild_ReturnsBufferNotString(t *testing.T) {
	t.Parallel()

	// Create a temporary directory with a valid kustomization
	tmpDir := t.TempDir()

	// Create a simple resource
	configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`

	err := os.WriteFile(filepath.Join(tmpDir, "configmap.yaml"), []byte(configMapYAML), 0o600)
	if err != nil {
		t.Fatalf("failed to write configmap: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(tmpDir, "kustomization.yaml"),
		[]byte(simpleConfigMapKustomization),
		0o600,
	)
	if err != nil {
		t.Fatalf("failed to write kustomization: %v", err)
	}

	// Test build
	client := kustomize.NewClient()
	ctx := context.Background()

	output, err := client.Build(ctx, tmpDir)
	if err != nil {
		t.Fatalf("expected build to succeed, got error: %v", err)
	}

	// Verify we get a *bytes.Buffer
	if output == nil {
		t.Fatal("expected non-nil buffer")
	}

	// Verify it has the correct type
	if output.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}
}

// writeSchemaExercisingKustomization writes a kustomization whose build races the
// kyaml openapi builtin-schema global on both sides: a strategic-merge patch on a
// Deployment forces SchemaForResourceType → the lazy init that WRITES the
// namespaceability map (findNamespaceability), and a custom-kind (non-precomputed)
// resource forces resWrangler.Append → IsNamespaceScoped → the UNLOCKED read of
// that same map. A plain resource passthrough (e.g. a ConfigMap, all precomputed)
// exercises neither, so the concurrent race test needs this shape to be meaningful.
func writeSchemaExercisingKustomization(t *testing.T, dir, name string) {
	t.Helper()

	// A strategic-merge patch on a Deployment forces SchemaForResourceType → the
	// one-time builtin-schema init that WRITES the namespaceability map
	// (findNamespaceability, under a lock it releases per-caller).
	files := map[string]string{
		"deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + name +
			"\n  namespace: default\nspec:\n  selector:\n    matchLabels:\n      app: " + name +
			"\n  template:\n    metadata:\n      labels:\n        app: " + name +
			"\n    spec:\n      containers:\n        - name: app\n          image: nginx:latest\n",
		"patch.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + name +
			"\n  namespace: default\nspec:\n  replicas: 2\n",
	}

	var resources strings.Builder

	resources.WriteString("resources:\n  - deployment.yaml\n")

	// Many custom, non-precomputed kinds: each is looked up via the UNLOCKED map
	// read (IsNamespaceScoped) that races the init write above. A high count keeps
	// reads flowing across the init window, so the race reproduces reliably (a
	// single custom resource almost never overlaps the brief one-time init).
	const widgets = 120
	for i := range widgets {
		fname := "widget-" + strconv.Itoa(i) + ".yaml"
		files[fname] = "apiVersion: example.com/v1\nkind: Widget" + strconv.Itoa(i) +
			"\nmetadata:\n  name: " + name + "-" + strconv.Itoa(i) + "\n"

		resources.WriteString("  - " + fname + "\n")
	}

	files["kustomization.yaml"] = "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n" +
		resources.String() + "patches:\n  - path: patch.yaml\n"

	for filename, content := range files {
		err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o600)
		if err != nil {
			t.Fatalf("failed to write %s: %v", filename, err)
		}
	}
}

// raceChildEnv marks the re-exec'd child process of
// TestBuild_ConcurrentBuildsNoRace.
const raceChildEnv = "KSAIL_KUSTOMIZE_RACE_CHILD"

// TestBuild_ConcurrentBuildsNoRace guards the kyaml openapi builtin-schema global
// against a lazy-first-init data race across concurrent builds (#5858).
//
// It re-execs itself in a fresh subprocess so the global starts *unwarmed*.
// Another Build test in this package could otherwise warm the process-global
// schema before this test runs, which would let the test pass even if
// Client.Build stopped pre-warming — masking a regression. The child runs the
// concurrent builds from a clean process; under `go test -race` it fails (and the
// parent surfaces the report) if those builds race.
func TestBuild_ConcurrentBuildsNoRace(t *testing.T) {
	if os.Getenv(raceChildEnv) == "1" {
		runConcurrentBuilds(t)

		return
	}

	t.Parallel()

	args := []string{"-test.run=^TestBuild_ConcurrentBuildsNoRace$", "-test.v"}

	// #nosec G204 G702 -- re-exec of this test binary (os.Args[0]) with a static
	// argv slice; no shell is invoked, so there is no command-injection surface.
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, raceChildEnv+"=1")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("concurrent-build race subprocess failed: %v\n%s", err, out)
	}
}

// runConcurrentBuilds builds several kustomizations concurrently and reports any
// error. It is the body executed by the re-exec'd child of
// TestBuild_ConcurrentBuildsNoRace.
func runConcurrentBuilds(t *testing.T) {
	t.Helper()

	const builders = 16

	dirs := make([]string, builders)
	for i := range dirs {
		dir := t.TempDir()
		writeSchemaExercisingKustomization(t, dir, "app-"+strconv.Itoa(i))
		dirs[i] = dir
	}

	client := kustomize.NewClient()
	ctx := context.Background()

	errCh := make(chan error, builders)

	var waitGroup sync.WaitGroup

	waitGroup.Add(builders)

	for _, dir := range dirs {
		go func() {
			defer waitGroup.Done()

			_, err := client.Build(ctx, dir)
			if err != nil {
				errCh <- err
			}
		}()
	}

	waitGroup.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent build failed: %v", err)
	}
}
