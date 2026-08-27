package helm_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

const admissionRegistrationGroup = "admissionregistration.k8s.io"

func TestResolveAPIVersionMigrations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		resources     []*metav1.APIResourceList
		expectedCount int
		expectedError string
	}{
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
			name:          "fails when neither API is served",
			expectedError: "no served API version",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			discoveryClient := &fake.FakeDiscovery{Fake: &k8stesting.Fake{}}
			discoveryClient.Resources = testCase.resources

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
