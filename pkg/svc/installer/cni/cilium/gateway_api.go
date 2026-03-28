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

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed Dockerfile.gateway-api
var gatewayAPIDockerfile string

// gatewayAPICRDsVersion returns the pinned Gateway API version extracted from the embedded Dockerfile.
func gatewayAPICRDsVersion() string {
	return parser.ParseImageFromDockerfile(
		gatewayAPIDockerfile,
		`FROM\s+registry\.k8s\.io/gateway-api/admission-server:v([^\s]+)`,
		"gateway-api",
	)
}

// gatewayAPICRDsURL returns the URL for the standard Gateway API CRDs bundle.
func gatewayAPICRDsURL() string {
	return "https://github.com/kubernetes-sigs/gateway-api/releases/download/v" +
		gatewayAPICRDsVersion() + "/standard-install.yaml"
}

// installGatewayAPICRDs installs the standard Gateway API CRDs required by Cilium.
// Cilium requires these CRDs to be pre-installed when gatewayAPI.enabled is true.
// See: https://docs.cilium.io/en/v1.19/network/servicemesh/gateway-api/gateway-api/#prerequisites
func (c *Installer) installGatewayAPICRDs(ctx context.Context) error {
	crds, err := fetchGatewayAPICRDs(ctx, gatewayAPICRDsURL())
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
		existing, getErr := client.ApiextensionsV1().
			CustomResourceDefinitions().
			Get(ctx, crd.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			_, createErr := client.ApiextensionsV1().
				CustomResourceDefinitions().
				Create(ctx, &crd, metav1.CreateOptions{})
			if createErr != nil {
				return fmt.Errorf("create CRD %s: %w", crd.Name, createErr)
			}

			continue
		}

		if getErr != nil {
			return fmt.Errorf("get CRD %s: %w", crd.Name, getErr)
		}

		crd.ResourceVersion = existing.ResourceVersion

		_, updateErr := client.ApiextensionsV1().
			CustomResourceDefinitions().
			Update(ctx, &crd, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("update CRD %s: %w", crd.Name, updateErr)
		}
	}

	return nil
}

// errUnexpectedHTTPStatus is returned when the CRD download receives a non-200 response.
var errUnexpectedHTTPStatus = errors.New("unexpected HTTP status")

// httpTimeout is the maximum duration for downloading Gateway API CRDs.
const httpTimeout = 30 * time.Second

// fetchGatewayAPICRDs downloads and parses CRDs from the Gateway API release bundle.
func fetchGatewayAPICRDs(
	ctx context.Context,
	url string,
) ([]apiextensionsv1.CustomResourceDefinition, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpClient := &http.Client{Timeout: httpTimeout}

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

	body, err := io.ReadAll(resp.Body)
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
