package vulnidentity_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/vulnidentity"
	"github.com/stretchr/testify/require"
)

// Query fixtures execute the already linked test binary. Writing only data
// avoids executing a newly written script while parallel forks may retain its
// writable descriptor on Linux.
func TestMain(tests *testing.M) {
	if len(os.Args) >= 6 && os.Args[1] == "-mode" && os.Args[2] == "query" &&
		os.Args[3] == "-json" && os.Args[4] == "-db" {
		output, err := readQueryFixture(os.Args[5])
		if err != nil {
			os.Exit(2)
		}

		_, err = os.Stdout.Write(output)
		if err != nil {
			os.Exit(2)
		}

		if strings.HasSuffix(os.Args[5], ".fail") {
			os.Exit(3)
		}

		os.Exit(0)
	}

	os.Exit(tests.Run())
}

func readQueryFixture(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("open query fixture root: %w", err)
	}

	output, readErr := root.ReadFile(filepath.Base(path))
	closeErr := root.Close()

	return output, errors.Join(readErr, closeErr)
}

// A failed process must remain a transport error even when it emits plausible
// complete scanner output before failing; it cannot clear the observed module.
func TestQueryRejectsCompleteOutputFromFailedProcess(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "output.json.fail")

	const output = `{"config":{"scanner_name":"govulncheck","scanner_version":"v1.8.0",
"scan_mode":"query","protocol_version":"v1.0.0"}}
{"progress":{"message":"Looking up vulnerabilities in example.test/dependency at v1.2.3..."}}`
	writeFixture(t, root, "output.json.fail", output)

	scanner, err := os.Executable()
	require.NoError(t, err)
	// The command executes this immutable test binary with an owned data fixture.
	command := exec.CommandContext( //nolint:gosec // This immutable test binary executes only the owned data fixture.
		t.Context(),
		scanner,
		"-mode",
		"query",
		"-json",
		"-db",
		path,
	)
	emitted, err := command.Output()

	var processError *exec.ExitError
	require.ErrorAs(t, err, &processError)
	require.Equal(t, 3, processError.ExitCode())
	// Scanner output is a JSONL stream, not one JSON document.
	require.Equal(
		t,
		strings.Split(output, "\n"),
		strings.Split(string(emitted), "\n"),
		"the transport control must really emit complete output",
	)
	findings, err := vulnidentity.Query(
		t.Context(),
		scanner,
		path,
		[]string{"example.test/dependency@v1.2.3"},
	)
	require.Nil(t, findings)
	require.ErrorAs(t, err, &processError)
	require.Equal(
		t,
		3,
		processError.ExitCode(),
		"the fixture must emit its output and take the explicit failure path",
	)
	require.NotErrorIs(
		t,
		err,
		vulnidentity.ErrObservation,
		"process failure is not parser rejection",
	)
}

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
			if name == "partial JSON" {
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			} else {
				require.ErrorIs(t, err, vulnidentity.ErrObservation)
			}
		})
	}

	findings, err := runQueryFixture(t, config+progress, identities)
	require.NoError(t, err)
	require.Empty(t, findings)

	_, err = runQueryFixture(t, config+progress, []string{
		"example.test/dependency@v1.2.3", "example.test/other@v1.0.0",
	})
	require.ErrorIs(t, err, vulnidentity.ErrObservation,
		"one successful observation cannot clear an unexamined second module")
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

	scanner, err := os.Executable()
	require.NoError(t, err)

	findings, err := vulnidentity.Query(
		t.Context(),
		scanner,
		filepath.Join(root, "output.json"),
		identities,
	)

	var (
		startError   *os.PathError
		processError *exec.ExitError
	)

	require.NotErrorAs(t, err, &startError, "fixture must start before examining its output")
	require.NotErrorAs(t, err, &processError, "fixture must complete before examining its output")

	if err != nil {
		return nil, fmt.Errorf("exercise scanner fixture: %w", err)
	}

	return findings, nil
}
