package ciharness_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportMetadataRejectsIncompleteOrRootImageReferences(t *testing.T) {
	t.Parallel()
	requireImportMetadataTools(t)

	for _, mode := range []string{
		"valid", "root-image", "missing-name", "missing-digest", "wrong-target",
		"unreadable-inventory", "bad-header", "duplicate-index", "non-ci", "wrong-node",
	} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			command := importMetadataCommand(t, mode)

			output, err := command.CombinedOutput()
			if mode == "valid" {
				require.NoError(t, err, "%s", output)
				assert.Contains(t, string(output), "Validated archive-root absence")
			} else {
				require.Error(t, err, "%s", output)
				assert.NotContains(t, string(output), "Validated archive-root absence")
			}
		})
	}
}

// requireImportMetadataTools checks the host utilities the script needs before
// any case runs. A missing tool would make every rejection case pass for the
// wrong reason, so the suite skips locally and fails on CI, where both exist.
func requireImportMetadataTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"jq", "sha256sum"} {
		_, err := exec.LookPath(tool)
		if err == nil {
			continue
		}

		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatalf("%s is required by assert-import-metadata.sh: %v", tool, err)
		}

		t.Skipf("%s is not installed; assert-import-metadata.sh needs it", tool)
	}
}

func importMetadataCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	directory := t.TempDir()
	bin := filepath.Join(directory, "bin")
	require.NoError(t, os.Mkdir(bin, 0o700))

	child := "sha256:" + strings.Repeat("a", 64)
	index := fmt.Sprintf(
		`{"schemaVersion":2,"manifests":[{"digest":%q,`+
			`"annotations":{"io.containerd.image.name":"example.test/app:fixture"}}]}`,
		child,
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(directory, "index.json"), []byte(index), 0o600),
	)
	root := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(index)))
	inventory := "REF TYPE DIGEST SIZE PLATFORMS LABELS\n" +
		"example.test/app:fixture application/vnd.oci.image.manifest.v1+json " + child + " 42 linux/amd64 -\n" +
		"import-fixture@" + child + " application/vnd.oci.image.manifest.v1+json " + child + " 42 linux/amd64 -\n"
	require.NoError(
		t,
		os.WriteFile(filepath.Join(directory, "inventory"), []byte(inventory), 0o600),
	)
	writeImportMetadataStubs(t, bin)

	// Take the identity from the CI matrix so the script guard and the system-test
	// host it runs on cannot drift apart.
	cluster := nestedKindRoutingScenario()["image-export-cluster-name"]
	require.Regexp(t, `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`, cluster,
		"the matrix cluster name must be a plain DNS label before it reaches the script")

	//nolint:gosec // Repository-owned script; its only variable inputs are the label checked above.
	command := exec.CommandContext(t.Context(), "bash",
		"../../.github/actions/ksail-test-image-export-import/assert-import-metadata.sh",
		cluster+"-control-plane", cluster+"-worker")

	command.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"), "RUNNER_TEMP="+directory,
		"GITHUB_ACTIONS=true", "RUNNER_ENVIRONMENT=github-hosted",
		"IMAGE_EXPORT_CLUSTER="+cluster, "IMPORT_METADATA_MODE="+mode,
		"IMPORT_METADATA_FIXTURE="+directory, "IMPORT_METADATA_ROOT="+root)
	if mode == "non-ci" {
		command.Env = append(command.Env, "GITHUB_ACTIONS=false")
	}

	if mode == "wrong-node" {
		command.Args[len(command.Args)-1] = "other-worker"
	}

	return command
}

func writeImportMetadataStubs(t *testing.T, bin string) {
	t.Helper()
	writeExecutableStub(t, filepath.Join(bin, "tar"), `#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  '-tf /tmp/test-images.tar')
    printf 'index.json\n'
    [[ "$IMPORT_METADATA_MODE" != duplicate-index ]] || printf 'index.json\n'
    ;;
  '-xOf /tmp/test-images.tar index.json') cat "$IMPORT_METADATA_FIXTURE/index.json" ;;
  *) exit 90 ;;
esac
`)
	writeExecutableStub(t, filepath.Join(bin, "docker"), `#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 6 && "$1" == exec && "$3 $4 $5 $6" == 'ctr --namespace=k8s.io images list' ]] || exit 91
case "$2" in
  "$IMAGE_EXPORT_CLUSTER-control-plane") cat "$IMPORT_METADATA_FIXTURE/inventory" ;;
  "$IMAGE_EXPORT_CLUSTER-worker")
    case "$IMPORT_METADATA_MODE" in
      root-image)
        cat "$IMPORT_METADATA_FIXTURE/inventory"
        printf 'import-root application/vnd.oci.image.index.v1+json %s 42 - -\n' "$IMPORT_METADATA_ROOT"
        ;;
      missing-name) grep -v '^example.test/' "$IMPORT_METADATA_FIXTURE/inventory" ;;
      missing-digest) grep -v '^import-' "$IMPORT_METADATA_FIXTURE/inventory" ;;
      wrong-target) sed 's/sha256:a/sha256:b/g' "$IMPORT_METADATA_FIXTURE/inventory" ;;
      unreadable-inventory) cat "$IMPORT_METADATA_FIXTURE/inventory"; exit 92 ;;
      bad-header) tail -n +2 "$IMPORT_METADATA_FIXTURE/inventory" ;;
      *) cat "$IMPORT_METADATA_FIXTURE/inventory" ;;
    esac ;;
  *) exit 93 ;;
esac
`)
}

// TestImportMetadataClusterInputReachesTheScripts pins the mapping the metadata test above
// bypasses by setting IMAGE_EXPORT_CLUSTER directly: if either action stopped forwarding the
// cluster name, CI would skip the metadata assertion without that test failing.
func TestImportMetadataClusterInputReachesTheScripts(t *testing.T) {
	t.Parallel()

	stepByID := func(action compositeAction, stepID string) harnessStep {
		t.Helper()

		for _, step := range action.Runs.Steps {
			if step.ID == stepID {
				return step
			}
		}

		require.Failf(t, "step not found", "no step with id %q", stepID)

		return harnessStep{}
	}

	systemTest := readCompositeAction(t, ".github/actions/ksail-system-test/action.yaml")
	require.Contains(t, systemTest.Inputs, "image-export-cluster-name")
	assert.Equal(t, "${{ inputs.image-export-cluster-name }}",
		stepByID(systemTest, "image-export-import-tests").With["multi-node-cluster-name"])

	exportImport := readCompositeAction(t,
		".github/actions/ksail-test-image-export-import/action.yaml")
	require.Contains(t, exportImport.Inputs, "multi-node-cluster-name")

	for _, id := range []string{"import", "multi-node"} {
		assert.Equal(t, "${{ inputs.multi-node-cluster-name }}",
			stepByID(exportImport, id).Env["IMAGE_EXPORT_CLUSTER"], "step %s", id)
	}
}
