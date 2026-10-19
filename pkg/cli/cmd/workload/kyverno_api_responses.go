package workload

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	"sigs.k8s.io/yaml"
)

const (
	kyvernoAPICallResponsesAPIVersion = "ksail.dev/v1alpha1"
	kyvernoAPICallResponsesKind       = "KyvernoAPICallResponses"
)

var (
	errUnexpectedKyvernoAPICallResponsesType = errors.New(
		"unexpected Kyverno API-call responses type",
	)
	errInvalidKyvernoAPICallResponse   = errors.New("invalid Kyverno API-call response")
	errDuplicateKyvernoAPICallResponse = errors.New("duplicate response")
)

type kyvernoAPICallResponsesFile struct {
	APIVersion string                           `json:"apiVersion"`
	Kind       string                           `json:"kind"`
	Responses  []kyvernoAPICallResponseDocument `json:"responses"`
}

type kyvernoAPICallResponseDocument struct {
	Method  string `json:"method"`
	URLPath string `json:"urlPath"`
	Body    any    `json:"body"`
}

func loadKyvernoAPICallResponses(
	root string,
	fixturePath string,
) (kyvernopolicy.APICallResponses, error) {
	if fixturePath == "" {
		return nil, nil //nolint:nilnil // A missing optional fixture is distinct from an empty fixture.
	}

	root, resolved, err := resolveKyvernoAPICallResponsesFile(root, fixturePath)
	if err != nil {
		return nil, err
	}

	data, err := fsutil.ReadFileSafe(root, resolved)
	if err != nil {
		return nil, fmt.Errorf("read Kyverno API-call responses %q: %w", fixturePath, err)
	}

	fixture, err := decodeKyvernoAPICallResponses(data, fixturePath)
	if err != nil {
		return nil, err
	}

	return buildKyvernoAPICallResponses(fixture.Responses, fixturePath)
}

// resolveKyvernoAPICallResponsesFile returns the directory a fixture path is relative
// to (the validation root, or the directory holding a single-file root) and the
// fixture file that path names.
func resolveKyvernoAPICallResponsesFile(root, fixturePath string) (string, string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", "", fmt.Errorf("inspect validation root %q: %w", root, err)
	}

	if !info.IsDir() {
		root = filepath.Dir(root)
	}

	resolved := fixturePath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, resolved)
	}

	return root, resolved, nil
}

func decodeKyvernoAPICallResponses(
	data []byte,
	fixturePath string,
) (kyvernoAPICallResponsesFile, error) {
	var fixture kyvernoAPICallResponsesFile

	err := yaml.UnmarshalStrict(data, &fixture)
	if err != nil {
		return kyvernoAPICallResponsesFile{}, fmt.Errorf(
			"decode Kyverno API-call responses %q: %w",
			fixturePath,
			err,
		)
	}

	if fixture.APIVersion != kyvernoAPICallResponsesAPIVersion ||
		fixture.Kind != kyvernoAPICallResponsesKind {
		return kyvernoAPICallResponsesFile{}, fmt.Errorf(
			"%w in %q: expected %s %s",
			errUnexpectedKyvernoAPICallResponsesType,
			fixturePath,
			kyvernoAPICallResponsesAPIVersion,
			kyvernoAPICallResponsesKind,
		)
	}

	return fixture, nil
}

func buildKyvernoAPICallResponses(
	documents []kyvernoAPICallResponseDocument,
	fixturePath string,
) (kyvernopolicy.APICallResponses, error) {
	responses := make(kyvernopolicy.APICallResponses, len(documents))
	for index, document := range documents {
		request, body, err := parseKyvernoAPICallResponse(document)
		if err != nil {
			return nil, fmt.Errorf("response %d in %q: %w", index+1, fixturePath, err)
		}

		if _, duplicate := responses[request]; duplicate {
			return nil, fmt.Errorf(
				"%w for %s %s in %q",
				errDuplicateKyvernoAPICallResponse,
				request.Method,
				request.URLPath,
				fixturePath,
			)
		}

		responses[request] = body
	}

	return responses, nil
}

func parseKyvernoAPICallResponse(
	document kyvernoAPICallResponseDocument,
) (kyvernopolicy.APICallRequest, json.RawMessage, error) {
	method := strings.ToUpper(strings.TrimSpace(document.Method))
	if method == "" {
		method = "GET"
	}

	if (method != "GET" && method != "POST") ||
		!strings.HasPrefix(document.URLPath, "/") || document.Body == nil {
		return kyvernopolicy.APICallRequest{}, nil, fmt.Errorf(
			"%w: requires GET or POST, an absolute urlPath, and a non-null body",
			errInvalidKyvernoAPICallResponse,
		)
	}

	body, err := json.Marshal(document.Body)
	if err != nil {
		return kyvernopolicy.APICallRequest{}, nil, fmt.Errorf("encode response body: %w", err)
	}

	request := kyvernopolicy.APICallRequest{Method: method, URLPath: document.URLPath}

	return request, body, nil
}

// kyvernoAPICallFixture is a loaded offline response fixture together with the
// file it was read from, so directory validation can leave that file out.
type kyvernoAPICallFixture struct {
	responses kyvernopolicy.APICallResponses
	// path is the resolved fixture file; empty when no fixture is configured.
	path string
}

// loadKyvernoAPICallFixture loads the configured fixture and records its file.
func loadKyvernoAPICallFixture(root, fixturePath string) (kyvernoAPICallFixture, error) {
	responses, err := loadKyvernoAPICallResponses(root, fixturePath)
	if err != nil {
		return kyvernoAPICallFixture{}, err
	}

	if fixturePath == "" {
		return kyvernoAPICallFixture{}, nil
	}

	_, resolved, err := resolveKyvernoAPICallResponsesFile(root, fixturePath)
	if err != nil {
		return kyvernoAPICallFixture{}, err
	}

	return kyvernoAPICallFixture{responses: responses, path: resolved}, nil
}

// excludeFixtureFile removes the fixture file from the loose YAML files. The
// fixture is a KSail input, not a Kubernetes workload, so kubeconform and CEL
// rules must not evaluate it. Paths are compared in absolute form; a path that
// cannot be made absolute is kept, so validation never silently skips a file.
func excludeFixtureFile(yamlFiles []string, fixturePath string) []string {
	if fixturePath == "" {
		return yamlFiles
	}

	fixture, err := filepath.Abs(fixturePath)
	if err != nil {
		return yamlFiles
	}

	filtered := make([]string, 0, len(yamlFiles))

	for _, file := range yamlFiles {
		absolute, err := filepath.Abs(file)
		if err == nil && absolute == fixture {
			continue
		}

		filtered = append(filtered, file)
	}

	return filtered
}
