package kyvernopolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	engineapi "github.com/kyverno/kyverno/pkg/engine/api"
	enginecontext "github.com/kyverno/kyverno/pkg/engine/context"
	"github.com/kyverno/kyverno/pkg/engine/factories"
	"github.com/kyverno/kyverno/pkg/engine/jmespath"
)

var errAPICallResponseMissing = errors.New("no offline response")

// APICallRequest identifies one Kubernetes API request made by a Kyverno
// context entry during offline policy evaluation.
type APICallRequest struct {
	Method  string
	URLPath string
}

// APICallResponses maps exact Kubernetes API requests to reviewed JSON
// responses. It is deliberately not a network fallback: an absent key is an
// evaluation error.
type APICallResponses map[APICallRequest]json.RawMessage

// EngineOption configures optional offline policy-evaluation context.
type EngineOption func(*engineOptions)

type engineOptions struct {
	apiCallResponses APICallResponses
}

// WithAPICallResponses enables Kubernetes apiCall context entries backed only
// by responses supplied here. Service calls remain disabled.
func WithAPICallResponses(responses APICallResponses) EngineOption {
	return func(options *engineOptions) {
		options.apiCallResponses = responses
	}
}

type apiCallResponseClient struct {
	responses APICallResponses
}

func newAPICallResponseClient(responses APICallResponses) *apiCallResponseClient {
	cloned := make(APICallResponses, len(responses))
	for request, response := range responses {
		request.Method = normalizedMethod(request.Method)

		cloned[request] = append(json.RawMessage(nil), response...)
	}

	return &apiCallResponseClient{responses: cloned}
}

func (c *apiCallResponseClient) RawAbsPath(
	_ context.Context,
	urlPath string,
	method string,
	_ io.Reader,
) ([]byte, error) {
	method = normalizedMethod(method)

	response, found := c.responses[APICallRequest{Method: method, URLPath: urlPath}]
	if !found {
		return nil, fmt.Errorf("%w for %s %s", errAPICallResponseMissing, method, urlPath)
	}

	return append([]byte(nil), response...), nil
}

func normalizedMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "GET"
	}

	return method
}

type apiCallFixtureLoader struct {
	delegate engineapi.ContextLoader
	client   engineapi.RawClient
}

func (l *apiCallFixtureLoader) Load(
	ctx context.Context,
	query jmespath.Interface,
	_ engineapi.RawClient,
	rclientFactory engineapi.RegistryClientFactory,
	entries []kyvernov1.ContextEntry,
	jsonContext enginecontext.Interface,
) error {
	err := l.delegate.Load(ctx, query, l.client, rclientFactory, entries, jsonContext)
	if err != nil {
		return fmt.Errorf("load Kyverno API-call fixture context: %w", err)
	}

	return nil
}

func apiCallFixtureLoaderFactory(responses APICallResponses) engineapi.ContextLoaderFactory {
	base := factories.DefaultContextLoaderFactory(nil)
	client := newAPICallResponseClient(responses)

	return func(policy kyvernov1.PolicyInterface, rule kyvernov1.Rule) engineapi.ContextLoader {
		return &apiCallFixtureLoader{delegate: base(policy, rule), client: client}
	}
}
