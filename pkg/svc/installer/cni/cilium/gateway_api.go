package ciliuminstaller

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/devantler-tech/ksail/v6/pkg/client/netretry"
	"github.com/devantler-tech/ksail/v6/pkg/svc/image/parser"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed Dockerfile.gateway-api
var gatewayAPIDockerfile string

const (
	// Retry Gateway API bundle downloads on transient GitHub/network failures in CI.
	gatewayAPICRDFetchMaxRetries = 5
	gatewayAPICRDRetryBaseWait   = 3 * time.Second
	gatewayAPICRDRetryMaxWait    = 30 * time.Second
	maxGatewayAPICRDResponseSize = 10 << 20
)

// gatewayAPICRDsVersion returns the pinned Gateway API version extracted from the embedded Dockerfile.
func gatewayAPICRDsVersion() string {
	return parser.ParseImageFromDockerfile(
		gatewayAPIDockerfile,
		`FROM\s+registry\.k8s\.io/gateway-api/admission-server:v([^\s]+)`,
		"gateway-api",
	)
}

// gatewayAPICRDsURL returns the URL for the experimental Gateway API CRDs bundle.
// The experimental bundle is required because Cilium's gateway controller uses TLSRoute
// (gateway.networking.k8s.io/v1alpha2), which is in the experimental channel and absent
// from standard-install.yaml. Without it, Cilium 1.15+ crashes on startup when
// gatewayAPI.enabled is true.
func gatewayAPICRDsURL() string {
	return "https://github.com/kubernetes-sigs/gateway-api/releases/download/v" +
		gatewayAPICRDsVersion() + "/experimental-install.yaml"
}

// installGatewayAPICRDs installs the experimental Gateway API CRDs required by Cilium.
// Cilium requires these CRDs to be pre-installed when gatewayAPI.enabled is true.
// See: https://docs.cilium.io/en/v1.19/network/servicemesh/gateway-api/gateway-api/#prerequisites
func (c *Installer) installGatewayAPICRDs(ctx context.Context) error {
	crds, err := fetchGatewayAPICRDs(ctx, gatewayAPICRDsURL(), c.GetTimeout())
	if err != nil {
		return fmt.Errorf("fetch Gateway API CRDs: %w", err)
	}

	restConfig, err := c.BuildRESTConfig()
	if err != nil {
		return fmt.Errorf("build REST config: %w", err)
	}

	client, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create apiextensions client: %w", err)
	}

	for _, crd := range crds {
		data, marshalErr := json.Marshal(&crd)
		if marshalErr != nil {
			return fmt.Errorf("marshal CRD %s: %w", crd.Name, marshalErr)
		}

		force := true

		_, patchErr := client.ApiextensionsV1().
			CustomResourceDefinitions().
			Patch(ctx, crd.Name, types.ApplyPatchType, data, metav1.PatchOptions{
				FieldManager: "ksail",
				Force:        &force,
			})
		if patchErr != nil {
			return fmt.Errorf("apply CRD %s: %w", crd.Name, patchErr)
		}
	}

	return nil
}

// errUnexpectedHTTPStatus is returned when the CRD download receives a non-200 response.
var errUnexpectedHTTPStatus = errors.New("unexpected HTTP status")

// fetchGatewayAPICRDs downloads and parses CRDs from the Gateway API release bundle.
func fetchGatewayAPICRDs(
	ctx context.Context,
	url string,
	timeout time.Duration,
) ([]apiextensionsv1.CustomResourceDefinition, error) {
	return fetchGatewayAPICRDsWithRetry(
		ctx,
		url,
		timeout,
		gatewayAPICRDFetchMaxRetries,
		gatewayAPICRDRetryBaseWait,
		gatewayAPICRDRetryMaxWait,
	)
}

func fetchGatewayAPICRDsWithRetry(
	ctx context.Context,
	url string,
	timeout time.Duration,
	maxRetries int,
	baseWait, maxWait time.Duration,
) ([]apiextensionsv1.CustomResourceDefinition, error) {
	// Clone the default transport to get an isolated connection pool.
	// This prevents CloseIdleConnections() calls on http.DefaultTransport
	// (e.g. from parallel tests or other goroutines) from breaking our
	// retry loop with "http: CloseIdleConnections called".
	// If http.DefaultTransport has been replaced with a custom RoundTripper,
	// fall back to it directly so that application-level overrides are respected.
	transport := http.DefaultTransport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = t.Clone()
	}

	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	retries := max(maxRetries, 1)

	var lastErr error

	for attempt := 1; attempt <= retries; attempt++ {
		crds, err := fetchGatewayAPICRDsOnce(ctx, url, httpClient)
		if err == nil {
			return crds, nil
		}

		lastErr = err
		if !netretry.IsRetryable(lastErr) || attempt == retries {
			break
		}

		delay := netretry.ExponentialDelay(attempt, baseWait, maxWait)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return nil, fmt.Errorf("download CRDs cancelled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return nil, lastErr
}

func fetchGatewayAPICRDsOnce(
	ctx context.Context,
	url string,
	httpClient *http.Client,
) ([]apiextensionsv1.CustomResourceDefinition, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download CRDs: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: %s downloading CRDs from %s",
			errUnexpectedHTTPStatus, resp.Status, url,
		)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayAPICRDResponseSize))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseGatewayAPICRDs(body)
}

// parseGatewayAPICRDs extracts CRD objects from a multi-document YAML bundle.
func parseGatewayAPICRDs(data []byte) ([]apiextensionsv1.CustomResourceDefinition, error) {
	var crds []apiextensionsv1.CustomResourceDefinition

	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read YAML document: %w", err)
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		jsonData, err := yaml.ToJSON(doc)
		if err != nil {
			return nil, fmt.Errorf("convert YAML to JSON: %w", err)
		}

		// Check if this document is a CRD by peeking at the kind field.
		var meta struct {
			Kind string `json:"kind"`
		}

		err = json.Unmarshal(jsonData, &meta)
		if err != nil {
			continue
		}

		if meta.Kind != "CustomResourceDefinition" {
			continue
		}

		crd := apiextensionsv1.CustomResourceDefinition{}

		err = json.Unmarshal(jsonData, &crd)
		if err != nil {
			return nil, fmt.Errorf("unmarshal CRD: %w", err)
		}

		crds = append(crds, crd)
	}

	return crds, nil
}
