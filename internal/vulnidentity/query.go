// Package vulnidentity preserves published vulnerability identities for local module replacements.
package vulnidentity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
)

var (
	// ErrIdentity identifies missing or invalid published module metadata.
	ErrIdentity = errors.New("invalid published module identity")
	// ErrObservation identifies missing, malformed or unexpected scanner evidence.
	ErrObservation = errors.New("incomplete vulnerability query observation")
	// ErrAdvisories identifies affected published local-source versions.
	ErrAdvisories = errors.New("published local-source versions have advisories")
	advisoryID    = regexp.MustCompile(`^GO-[0-9]{4}-[0-9]+$`)
)

// Query checks published versions independently of the locally replaced source
// graph. A successful process and complete query observations are both required;
// JSON mode alone exits successfully even when vulnerabilities are reported.
func Query(ctx context.Context, scanner, database string, identities []string) ([]string, error) {
	if len(identities) == 0 {
		return nil, fmt.Errorf("%w: no identities supplied", ErrIdentity)
	}

	arguments := []string{"-mode", "query", "-json"}
	if database != "" {
		arguments = append(arguments, "-db", database)
	}

	arguments = append(arguments, identities...)
	// Caller supplies the installed scanner; identities are arguments, never shell code.
	command := exec.CommandContext(ctx, scanner, arguments...)

	var stdout, stderr bytes.Buffer

	command.Stdout, command.Stderr = &stdout, &stderr

	err := command.Run()
	if err != nil {
		return nil, fmt.Errorf("vulnerability identity query failed: %w: %s", err, &stderr)
	}

	return validateQuery(&stdout, identities)
}

func validateQuery(output io.Reader, identities []string) ([]string, error) {
	wanted, err := expectedQueries(identities)
	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(output)
	configured := false

	var findings []string

	for {
		var message map[string]json.RawMessage

		err := decoder.Decode(&message)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("incomplete scanner JSON: %w", err)
		}

		if len(message) != 1 {
			return nil, fmt.Errorf("%w: invalid message", ErrObservation)
		}

		finding, messageErr := queryMessage(message, &configured, wanted)
		if messageErr != nil {
			return nil, messageErr
		}

		if finding != "" {
			findings = append(findings, finding)
		}
	}

	err = completeQueries(configured, wanted)
	if err != nil {
		return nil, err
	}

	slices.Sort(findings)

	return slices.Compact(findings), nil
}

func expectedQueries(identities []string) (map[string]bool, error) {
	wanted := make(map[string]bool, len(identities))
	for _, identity := range identities {
		path, version, found := strings.Cut(identity, "@")
		if !found || path == "" || version == "" {
			return nil, fmt.Errorf("%w: %s", ErrIdentity, identity)
		}

		message := fmt.Sprintf("Looking up vulnerabilities in %s at %s...", path, version)
		if _, duplicate := wanted[message]; duplicate {
			return nil, fmt.Errorf("%w: duplicate %s", ErrIdentity, identity)
		}

		wanted[message] = false
	}

	return wanted, nil
}

func completeQueries(configured bool, wanted map[string]bool) error {
	if !configured || len(wanted) == 0 {
		return fmt.Errorf("%w: missing configuration or module observations", ErrObservation)
	}

	for message, seen := range wanted {
		if !seen {
			return fmt.Errorf("%w: missing %s", ErrObservation, message)
		}
	}

	return nil
}

func queryMessage(
	message map[string]json.RawMessage,
	configured *bool,
	wanted map[string]bool,
) (string, error) {
	for kind, payload := range message {
		switch kind {
		case "config":
			return "", acceptConfiguration(payload, configured)
		case "progress":
			return "", acceptProgress(payload, *configured, wanted)
		case "osv":
			return readAdvisory(payload, *configured)
		default:
			return "", fmt.Errorf("%w: unexpected %s", ErrObservation, kind)
		}
	}

	return "", nil
}

func acceptConfiguration(payload json.RawMessage, configured *bool) error {
	if *configured {
		return fmt.Errorf("%w: repeated configuration", ErrObservation)
	}

	err := validateConfiguration(payload)
	if err != nil {
		return err
	}

	*configured = true

	return nil
}

func acceptProgress(payload json.RawMessage, configured bool, wanted map[string]bool) error {
	var progress struct {
		Message string `json:"message"`
	}

	err := json.Unmarshal(payload, &progress)
	if err != nil {
		return fmt.Errorf("invalid query observation: %w", err)
	}

	seen, expected := wanted[progress.Message]
	if !configured || !expected || seen {
		return fmt.Errorf("%w: unexpected or repeated query", ErrObservation)
	}

	wanted[progress.Message] = true

	return nil
}

func readAdvisory(payload json.RawMessage, configured bool) (string, error) {
	var advisory struct {
		ID string `json:"id"`
	}

	err := json.Unmarshal(payload, &advisory)
	if err != nil {
		return "", fmt.Errorf("invalid advisory: %w", err)
	}

	if !configured || !advisoryID.MatchString(advisory.ID) {
		return "", fmt.Errorf("%w: missing advisory identity", ErrObservation)
	}

	return advisory.ID, nil
}

// govulncheck defines these protocol keys in snake_case.
//
//nolint:tagliatelle
type scannerConfiguration struct {
	Name     string `json:"scanner_name"`
	Version  string `json:"scanner_version"`
	Mode     string `json:"scan_mode"`
	Protocol string `json:"protocol_version"`
}

func validateConfiguration(payload json.RawMessage) error {
	var config scannerConfiguration

	err := json.Unmarshal(payload, &config)
	if err != nil {
		return fmt.Errorf("invalid scanner configuration: %w", err)
	}

	if config.Name != "govulncheck" || config.Version != "v1.8.0" ||
		config.Mode != "query" || config.Protocol != "v1.0.0" {
		return fmt.Errorf("%w: unexpected scanner configuration", ErrObservation)
	}

	return nil
}
