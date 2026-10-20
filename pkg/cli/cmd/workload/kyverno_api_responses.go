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
	base string,
	fixturePath string,
) (kyvernopolicy.APICallResponses, error) {
	if fixturePath == "" {
		return nil, nil //nolint:nilnil // A missing optional fixture is distinct from an empty fixture.
	}

	resolved, err := resolveKyvernoAPICallResponsesFile(base, fixturePath)
	if err != nil {
		return nil, err
	}

	data, err := fsutil.ReadFileSafe(base, resolved)
	if err != nil {
		return nil, fmt.Errorf("read Kyverno API-call responses %q: %w", fixturePath, err)
	}

	fixture, err := decodeKyvernoAPICallResponses(data, fixturePath)
	if err != nil {
		return nil, err
	}

	return buildKyvernoAPICallResponses(fixture.Responses, fixturePath)
}

// resolveKyvernoAPICallResponsesFile returns the canonical file a fixture path
// names. A relative path is relative to base, the working directory KSail runs
// in, exactly like --rules and spec.workload.sourceDirectory. Symlinks are
// resolved so the result compares equal to the files the directory walk finds.
func resolveKyvernoAPICallResponsesFile(base, fixturePath string) (string, error) {
	resolved := fixturePath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, resolved)
	}

	canonical, err := fsutil.EvalCanonicalPath(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve Kyverno API-call responses %q: %w", fixturePath, err)
	}

	return canonical, nil
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
// base is the directory a relative fixture path resolves against and the
// directory the fixture must stay inside.
func loadKyvernoAPICallFixture(base, fixturePath string) (kyvernoAPICallFixture, error) {
	if fixturePath == "" {
		return kyvernoAPICallFixture{}, nil
	}

	responses, err := loadKyvernoAPICallResponses(base, fixturePath)
	if err != nil {
		return kyvernoAPICallFixture{}, err
	}

	resolved, err := resolveKyvernoAPICallResponsesFile(base, fixturePath)
	if err != nil {
		return kyvernoAPICallFixture{}, err
	}

	return kyvernoAPICallFixture{responses: responses, path: resolved}, nil
}

// kyvernoAPICallFixtureApplies reports whether a validation run consumes the
// fixture. Only the Kyverno pass over a directory's kustomizations reads it, so
// a single-file target or a run with Kyverno policies off never loads it.
func kyvernoAPICallFixtureApplies(kyvernoPolicies bool, target string) (bool, error) {
	if !kyvernoPolicies {
		return false, nil
	}

	info, err := os.Stat(target)
	if err != nil {
		return false, fmt.Errorf("inspect validation path %q: %w", target, err)
	}

	return info.IsDir(), nil
}

// excludeFixtureFile removes the fixture file from the loose YAML files. The
// fixture is a KSail input, not a Kubernetes workload, so kubeconform and CEL
// rules must not evaluate it. fixturePath is canonical; each file is
// canonicalized too, so a symlinked spelling of the fixture is still excluded.
// A file that cannot be canonicalized is kept, so validation never silently
// skips a file.
func excludeFixtureFile(yamlFiles []string, fixturePath string) []string {
	if fixturePath == "" {
		return yamlFiles
	}

	filtered := make([]string, 0, len(yamlFiles))

	for _, file := range yamlFiles {
		canonical, err := fsutil.EvalCanonicalPath(file)
		if err == nil && canonical == fixturePath {
			continue
		}

		filtered = append(filtered, file)
	}

	return filtered
}
