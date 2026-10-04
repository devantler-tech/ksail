package vulnidentity_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/vulnidentity"
	"github.com/stretchr/testify/require"
)

func TestQueryRejectsIncompleteObservation(t *testing.T) {
	t.Parallel()

	const (
		config = `{"config":{"scanner_name":"govulncheck","scanner_version":"v1.8.0",
"scan_mode":"query","protocol_version":"v1.0.0"}}`
		progress = `{"progress":{"message":"Looking up vulnerabilities in example.test/dependency at v1.2.3..."}}`
	)

	identities := []string{"example.test/dependency@v1.2.3"}

	for name, output := range map[string]string{
		"empty":            "",
		"no lookup":        config,
		"no config":        progress,
		"partial JSON":     config + progress + `{"osv":`,
		"wrong module":     config + strings.ReplaceAll(progress, "dependency", "other"),
		"duplicate lookup": config + progress + progress,
		"wrong scanner":    strings.ReplaceAll(config, "v1.8.0", "v1.7.0") + progress,
		"invalid advisory": config + progress + `{"osv":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := runQueryFixture(t, output, identities)
			require.Error(t, err)
		})
	}

	findings, err := runQueryFixture(t, config+progress, identities)
	require.NoError(t, err)
	require.Empty(t, findings)

	_, err = runQueryFixture(t, config+progress, []string{
		"example.test/dependency@v1.2.3", "example.test/other@v1.0.0",
	})
	require.Error(t, err, "one successful observation cannot clear an unexamined second module")
}

func TestQueryRetainsAllAdvisories(t *testing.T) {
	t.Parallel()

	const output = `{"config":{"scanner_name":"govulncheck","scanner_version":"v1.8.0",
"scan_mode":"query","protocol_version":"v1.0.0"}}
{"progress":{"message":"Looking up vulnerabilities in example.test/dependency at v1.2.3..."}}
{"osv":{"id":"GO-2022-0229"}}
{"osv":{"id":"GO-2022-0463"}}`

	findings, err := runQueryFixture(t, output, []string{"example.test/dependency@v1.2.3"})
	require.NoError(t, err)
	require.Equal(t, []string{"GO-2022-0229", "GO-2022-0463"}, findings)
}

func runQueryFixture(t *testing.T, output string, identities []string) ([]string, error) {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "output.json", output)
	writeFixture(t, root, "scanner", "#!/bin/bash\ncat \"$(dirname \"$0\")/output.json\"\n")
	scanner := filepath.Join(root, "scanner")
	// Only this test fixture's owner can execute it.
	require.NoError(t, os.Chmod(scanner, 0o700)) //nolint:gosec

	findings, err := vulnidentity.Query(t.Context(), scanner, "", identities)
	if err != nil {
		return nil, fmt.Errorf("exercise scanner fixture: %w", err)
	}

	return findings, nil
}
