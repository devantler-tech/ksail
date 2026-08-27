package helm_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

const admissionRegistrationGroup = "admissionregistration.k8s.io"

func TestResolveAPIVersionMigrations(t *testing.T) {
	t.Parallel()

	for _, testCase := range apiMigrationResolutionTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fakeDiscovery := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}}
			fakeDiscovery.Resources = testCase.resources
			discoveryClient := &groupVersionDiscovery{
				DiscoveryInterface: fakeDiscovery,
				resourceErrors:     testCase.resourceErrors,
			}

			count, err := helm.ResolveAPIVersionMigrationsForTest(
				discoveryClient,
				admissionAPIMigrations(),
			)

			if testCase.expectedError != "" {
				require.ErrorContains(t, err, testCase.expectedError)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedCount, count)
		})
	}
}

type apiMigrationResolutionTestCase struct {
	name           string
	resources      []*metav1.APIResourceList
	resourceErrors map[string]error
	expectedCount  int
	expectedError  string
}

func apiMigrationResolutionTestCases() []apiMigrationResolutionTestCase {
	return []apiMigrationResolutionTestCase{
		{
			name: "keeps served source API",
			resources: []*metav1.APIResourceList{
				apiResourceList(admissionRegistrationGroup+"/v1beta1", admissionKinds()...),
			},
			expectedCount: 0,
		},
		{
			name: "migrates to served target API",
			resources: []*metav1.APIResourceList{
				apiResourceList(admissionRegistrationGroup+"/v1", admissionKinds()...),
			},
			expectedCount: 2,
		},
		{
			name: "migrates after a source memory cache miss",
			resources: []*metav1.APIResourceList{
				apiResourceList(admissionRegistrationGroup+"/v1", admissionKinds()...),
			},
			resourceErrors: map[string]error{
				admissionRegistrationGroup + "/v1beta1": memory.ErrCacheNotFound,
			},
			expectedCount: 2,
		},
		{
			name:          "fails when neither API is served",
			expectedError: "no served API version",
		},
	}
}

type groupVersionDiscovery struct {
	discovery.DiscoveryInterface

	resourceErrors map[string]error
}

func (d *groupVersionDiscovery) ServerResourcesForGroupVersion(
	groupVersion string,
) (*metav1.APIResourceList, error) {
	resourceErr := d.resourceErrors[groupVersion]
	if resourceErr != nil {
		return nil, resourceErr
	}

	resources, err := d.DiscoveryInterface.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return nil, fmt.Errorf("discover delegated group version: %w", err)
	}

	return resources, nil
}

func TestAPIVersionPostRenderer_Run(t *testing.T) {
	t.Parallel()

	input := `# Source: calico/templates/policy.yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingAdmissionPolicy
metadata:
  name: calico-policy
---
# Source: calico/templates/binding.yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: MutatingAdmissionPolicyBinding
metadata:
  name: calico-binding
---
# Source: another-chart/templates/policy.yaml
apiVersion: admissionregistration.k8s.io/v1beta1
kind: ValidatingAdmissionPolicy
metadata:
  name: leave-untouched
`

	discoveryClient := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}}
	discoveryClient.Resources = []*metav1.APIResourceList{
		apiResourceList(admissionRegistrationGroup+"/v1", admissionKinds()...),
	}

	output, err := helm.RenderAPIVersionMigrationsForTest(
		discoveryClient,
		admissionAPIMigrations(),
		input,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(output, "apiVersion: admissionregistration.k8s.io/v1\n"))
	assert.Contains(
		t,
		output,
		"apiVersion: admissionregistration.k8s.io/v1beta1\nkind: ValidatingAdmissionPolicy",
	)
	assert.Contains(t, output, "# Source: calico/templates/policy.yaml")
}

func TestOptionalAPIVersionPostRenderer_DisablesNilRenderer(t *testing.T) {
	t.Parallel()

	assert.Nil(t, helm.OptionalAPIVersionPostRendererForTest(false))
	assert.NotNil(t, helm.OptionalAPIVersionPostRendererForTest(true))
}

func admissionAPIMigrations() []helm.APIVersionMigration {
	return []helm.APIVersionMigration{
		{
			Kind: "MutatingAdmissionPolicy",
			From: admissionRegistrationGroup + "/v1beta1",
			To:   admissionRegistrationGroup + "/v1",
		},
		{
			Kind: "MutatingAdmissionPolicyBinding",
			From: admissionRegistrationGroup + "/v1beta1",
			To:   admissionRegistrationGroup + "/v1",
		},
	}
}

func apiResourceList(groupVersion string, kinds ...string) *metav1.APIResourceList {
	resources := make([]metav1.APIResource, 0, len(kinds))
	for _, kind := range kinds {
		resources = append(resources, metav1.APIResource{Kind: kind})
	}

	return &metav1.APIResourceList{GroupVersion: groupVersion, APIResources: resources}
}

func admissionKinds() []string {
	return []string{"MutatingAdmissionPolicy", "MutatingAdmissionPolicyBinding"}
}
