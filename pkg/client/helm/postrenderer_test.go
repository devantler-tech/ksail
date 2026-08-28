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

func TestResolveFreshAPIVersionMigrationsInvalidatesStaleDiscovery(t *testing.T) {
	t.Parallel()

	fakeDiscovery := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}}
	fakeDiscovery.Resources = []*metav1.APIResourceList{
		apiResourceList(admissionRegistrationGroup+"/v1", admissionKinds()...),
	}
	discoveryClient := &staleCachedDiscovery{
		DiscoveryInterface: fakeDiscovery,
		staleResources: apiResourceList(
			admissionRegistrationGroup+"/v1beta1",
			admissionKinds()...,
		),
	}

	count, err := helm.ResolveFreshAPIVersionMigrationsForTest(
		discoveryClient,
		admissionAPIMigrations(),
	)

	require.NoError(t, err)
	assert.True(t, discoveryClient.invalidated)
	assert.Equal(t, 2, count)
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

type staleCachedDiscovery struct {
	discovery.DiscoveryInterface

	staleResources *metav1.APIResourceList
	invalidated    bool
}

func (d *staleCachedDiscovery) Fresh() bool { return d.invalidated }

func (d *staleCachedDiscovery) Invalidate() { d.invalidated = true }

func (d *staleCachedDiscovery) ServerResourcesForGroupVersion(
	groupVersion string,
) (*metav1.APIResourceList, error) {
	if !d.invalidated && groupVersion == d.staleResources.GroupVersion {
		return d.staleResources, nil
	}

	resources, err := d.DiscoveryInterface.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return nil, fmt.Errorf("discover refreshed group version: %w", err)
	}

	return resources, nil
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

func TestAPIVersionPostRenderer_PreservesQuotedAndCRLFFormatting(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "quoted API version",
			input: "apiVersion: \"admissionregistration.k8s.io/v1beta1\" # keep\n" +
				"kind: MutatingAdmissionPolicy\n",
			expected: "apiVersion: \"admissionregistration.k8s.io/v1\" # keep\n" +
				"kind: MutatingAdmissionPolicy\n",
		},
		{
			name: "single-quoted API version",
			input: "apiVersion: 'admissionregistration.k8s.io/v1beta1'\n" +
				"kind: MutatingAdmissionPolicyBinding\n",
			expected: "apiVersion: 'admissionregistration.k8s.io/v1'\n" +
				"kind: MutatingAdmissionPolicyBinding\n",
		},
		{
			name: "CRLF line endings",
			input: "apiVersion: admissionregistration.k8s.io/v1beta1\r\n" +
				"kind: MutatingAdmissionPolicyBinding\r\n",
			expected: "apiVersion: admissionregistration.k8s.io/v1\r\n" +
				"kind: MutatingAdmissionPolicyBinding\r\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			discoveryClient := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}}
			discoveryClient.Resources = []*metav1.APIResourceList{
				apiResourceList(admissionRegistrationGroup+"/v1", admissionKinds()...),
			}

			output, err := helm.RenderAPIVersionMigrationsForTest(
				discoveryClient,
				admissionAPIMigrations(),
				testCase.input,
			)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, output)
		})
	}
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
