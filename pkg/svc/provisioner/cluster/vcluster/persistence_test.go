package vclusterprovisioner_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func clientWithDefaultStorageClass(t *testing.T, hasDefault bool) kubernetes.Interface {
	t.Helper()

	if !hasDefault {
		return k8sfake.NewClientset()
	}

	return k8sfake.NewClientset(&storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "standard",
			Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
		},
	})
}

func writeUserValues(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "vcluster.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// persistenceValues builds a vcluster.yaml snippet setting one persistence volumeClaim field,
// e.g. claimLine "enabled: true" or "storageClass: fast".
func persistenceValues(claimLine string) string {
	return "controlPlane:\n  statefulSet:\n    persistence:\n      volumeClaim:\n        " +
		claimLine + "\n"
}

func TestResolvePersistenceDisabled(t *testing.T) {
	t.Parallel()

	persistentValues := persistenceValues("enabled: true")
	disabledValues := persistenceValues("enabled: false")
	autoValues := persistenceValues("enabled: auto")
	storageClassValues := persistenceValues("storageClass: fast")

	tests := map[string]struct {
		hasDefaultSC bool
		userValues   string // "" = no user file
		wantDisabled bool
		wantErr      bool
	}{
		"no user values, no StorageClass -> emptyDir":      {false, "", true, false},
		"no user values, StorageClass present -> keep PVC": {true, "", false, false},
		"explicit enabled:true, no StorageClass -> fail":   {false, persistentValues, false, true},
		"explicit enabled:true, StorageClass -> keep PVC":  {true, persistentValues, false, false},
		"explicit enabled:false, no StorageClass -> honor": {false, disabledValues, false, false},
		"explicit auto, no StorageClass -> emptyDir":       {false, autoValues, true, false},
		"explicit storageClass, no StorageClass -> fail": {
			false,
			storageClassValues,
			false,
			true,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := ""
			if testCase.userValues != "" {
				path = writeUserValues(t, testCase.userValues)
			}

			client := clientWithDefaultStorageClass(t, testCase.hasDefaultSC)

			disabled, err := vclusterprovisioner.ResolvePersistenceDisabledForTest(
				context.Background(), client, path,
			)

			if testCase.wantErr {
				require.ErrorIs(t, err, vclusterprovisioner.ErrPersistentStorageUnavailableForTest)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantDisabled, disabled)
		})
	}
}

func TestUserPersistenceIntent(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		content      string // "" = no file
		wantWants    bool
		wantDisables bool
	}{
		"no file":          {"", false, false},
		"enabled true":     {persistenceValues("enabled: true"), true, false},
		"enabled false":    {persistenceValues("enabled: false"), false, true},
		"enabled auto":     {persistenceValues("enabled: auto"), false, false},
		"storageClass set": {persistenceValues("storageClass: fast"), true, false},
		"unrelated values": {
			"controlPlane:\n  distro:\n    k8s:\n      image:\n        tag: v1.31.0\n",
			false,
			false,
		},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := ""
			if testCase.content != "" {
				path = writeUserValues(t, testCase.content)
			}

			wants, disables, err := vclusterprovisioner.UserPersistenceIntentForTest(path)
			require.NoError(t, err)
			assert.Equal(t, testCase.wantWants, wants)
			assert.Equal(t, testCase.wantDisables, disables)
		})
	}
}
