package helm

import (
	"bytes"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const admissionRegistrationGroup = "admissionregistration.k8s.io"

var admissionAPIMigrations = []APIVersionMigration{
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

			migrations, err := resolveAPIVersionMigrations(discoveryClient, admissionAPIMigrations)

			if testCase.expectedError != "" {
				require.ErrorContains(t, err, testCase.expectedError)

				return
			}

			require.NoError(t, err)
			assert.Len(t, migrations, testCase.expectedCount)
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

	renderer := &apiVersionPostRenderer{
		migrations: map[apiVersionMigrationKey]string{
			{apiVersion: admissionRegistrationGroup + "/v1beta1", kind: "MutatingAdmissionPolicy"}:        admissionRegistrationGroup + "/v1",
			{apiVersion: admissionRegistrationGroup + "/v1beta1", kind: "MutatingAdmissionPolicyBinding"}: admissionRegistrationGroup + "/v1",
		},
	}

	output, err := renderer.Run(bytes.NewBufferString(input))

	require.NoError(t, err)
	assert.Equal(t, 2, bytes.Count(output.Bytes(), []byte("apiVersion: admissionregistration.k8s.io/v1\n")))
	assert.Contains(t, output.String(), "apiVersion: admissionregistration.k8s.io/v1beta1\nkind: ValidatingAdmissionPolicy")
	assert.Contains(t, output.String(), "# Source: calico/templates/policy.yaml")
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
