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

	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect validation root %q: %w", root, err)
	}
	if !info.IsDir() {
		root = filepath.Dir(root)
	}

	resolved := fixturePath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, resolved)
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
