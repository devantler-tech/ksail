//go:build integration

package vulnidentity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/vulnidentity"
	"github.com/stretchr/testify/require"
)

// The actual pinned scanner and its authenticated upstream database fixture are
// mandatory inputs to the integration job, not optional test skips.
func TestActualScannerPreservesLocalReplacementAdvisories(t *testing.T) {
	t.Parallel()

	scanner, database := os.Getenv("GOVULNCHECK_BIN"), os.Getenv("GOVULNCHECK_TEST_DB")
	require.NotEmpty(t, scanner)
	require.NotEmpty(t, database)

	const (
		affected = "v0.0.0-20191002192127-34f69633bfdc"
		fixed    = "v0.0.0-20200124225646-8b5121be2f68"
	)

	root := t.TempDir()
	module := "module example.test/root\ngo 1.26\nrequire golang.org/x/crypto " + affected +
		"\nreplace golang.org/x/crypto " + affected + " => ./crypto\n"
	writeFixture(t, root, "go.mod", module)
	writeFixture(
		t,
		root,
		"main.go",
		"package main\nimport _ \"golang.org/x/crypto/cryptobyte\"\nfunc main() {}\n",
	)
	writeFixture(t, root, "crypto/go.mod", "module golang.org/x/crypto\ngo 1.26\n")
	writeFixture(t, root, "crypto/cryptobyte/byte.go", "package cryptobyte\n")

	verifyActualScannerBlindSpot(t, scanner, database, root)

	identities, err := vulnidentity.Collect(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, []string{"golang.org/x/crypto@" + affected}, identities)
	findings, err := vulnidentity.Query(t.Context(), scanner, database, identities)
	require.NoError(t, err)
	require.Equal(t, []string{"GO-2022-0229"}, findings)

	writeFixture(t, root, "go.mod", strings.ReplaceAll(module, affected, fixed))
	identities, err = vulnidentity.Collect(t.Context(), root)
	require.NoError(t, err)
	findings, err = vulnidentity.Query(t.Context(), scanner, database, identities)
	require.NoError(t, err)
	require.Empty(t, findings, "the actual fixed version must remain clean")

	verifyFailedActualQuery(t, scanner, database, identities, root)
}

func verifyActualScannerBlindSpot(t *testing.T, scanner, database, root string) {
	t.Helper()
	// The executable is a mandatory trusted integration-job input.
	baseline := exec.CommandContext( //nolint:gosec
		t.Context(),
		scanner,
		"-C",
		root,
		"-db",
		database,
		"-scan",
		"module",
		"-json",
	)
	baselineOutput, err := baseline.Output()
	require.NoError(t, err)

	decoder := json.NewDecoder(bytes.NewReader(baselineOutput))

	for {
		var observation struct {
			Finding *struct {
				OSV string `json:"osv"`
			} `json:"finding"`
		}

		err := decoder.Decode(&observation)
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		if observation.Finding != nil {
			require.NotEqual(t, "GO-2022-0229", observation.Finding.OSV,
				"characterize the real scanner's local replacement blind spot")
		}
	}
}

func verifyFailedActualQuery(
	t *testing.T,
	scanner, database string,
	identities []string,
	root string,
) {
	t.Helper()
	// Even a complete-looking clean query is rejected if the process failed.
	// The executable is a mandatory trusted integration-job input.
	clean := exec.CommandContext( //nolint:gosec
		t.Context(),
		scanner,
		"-mode",
		"query",
		"-json",
		"-db",
		database,
		identities[0],
	)
	cleanOutput, err := clean.Output()
	require.NoError(t, err)

	failed := filepath.Join(root, "failed-scanner")
	writeFixture(t, root, "clean.json", string(cleanOutput))
	writeFixture(
		t,
		root,
		"failed-scanner",
		"#!/bin/bash\ncat \"$(dirname \"$0\")/clean.json\"\nexit 1\n",
	)
	// Only this test fixture's owner can execute it.
	require.NoError(t, os.Chmod(failed, 0o700)) //nolint:gosec
	_, err = vulnidentity.Query(context.Background(), failed, database, identities)
	require.Error(t, err)

	truncated := bytes.TrimSpace(cleanOutput)
	_, err = runQueryFixture(t, string(truncated[:len(truncated)-1]), identities)
	require.Error(t, err)
}
