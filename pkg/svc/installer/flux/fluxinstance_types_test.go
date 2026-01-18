package fluxinstaller_test

import (
	"testing"
	"time"

	fluxinstaller "github.com/devantler-tech/ksail/v5/pkg/svc/installer/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test strings.
const (
	modifiedValue = "modified"
	testSyncName  = "test-sync"
)

func TestFluxInstance_DeepCopy(t *testing.T) {
	t.Parallel()

	original := &fluxinstaller.FluxInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "test-namespace",
		},
		Spec: fluxinstaller.FluxInstanceSpec{
			Distribution: fluxinstaller.Distribution{
				Version:  "2.x",
				Registry: "ghcr.io/fluxcd",
				Artifact: "oci://example.com/flux",
			},
			Sync: &fluxinstaller.Sync{
				Name:     "test-sync",
				Kind:     "OCIRepository",
				URL:      "oci://example.com/repo",
				Ref:      "dev",
				Path:     "./",
				Provider: "generic",
				Interval: &metav1.Duration{Duration: time.Minute},
			},
		},
		Status: fluxinstaller.FluxInstanceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	copied := original.DeepCopy()

	require.NotNil(t, copied)
	assert.Equal(t, original.Name, copied.Name)
	assert.Equal(t, original.Namespace, copied.Namespace)
	assert.Equal(t, original.Spec.Distribution.Version, copied.Spec.Distribution.Version)
	assert.Equal(t, original.Spec.Sync.URL, copied.Spec.Sync.URL)
	assert.Len(t, copied.Status.Conditions, 1)

	// Verify deep copy - modifications to copy don't affect original
	copied.Name = modifiedValue
	copied.Spec.Distribution.Version = modifiedValue
	assert.NotEqual(t, original.Name, copied.Name)
	assert.NotEqual(t, original.Spec.Distribution.Version, copied.Spec.Distribution.Version)
}

func TestFluxInstance_DeepCopy_Nil(t *testing.T) {
	t.Parallel()

	var original *fluxinstaller.FluxInstance

	copied := original.DeepCopy()

	assert.Nil(t, copied)
}

func TestFluxInstance_DeepCopyObject(t *testing.T) {
	t.Parallel()

	original := &fluxinstaller.FluxInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	obj := original.DeepCopyObject()

	require.NotNil(t, obj)
	copied, ok := obj.(*fluxinstaller.FluxInstance)
	require.True(t, ok)
	assert.Equal(t, original.Name, copied.Name)
}

func TestFluxInstanceList_DeepCopy(t *testing.T) {
	t.Parallel()

	original := &fluxinstaller.FluxInstanceList{
		Items: []fluxinstaller.FluxInstance{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "item1"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "item2"},
			},
		},
	}

	copied := original.DeepCopy()

	require.NotNil(t, copied)
	assert.Len(t, copied.Items, 2)
	assert.Equal(t, "item1", copied.Items[0].Name)
	assert.Equal(t, "item2", copied.Items[1].Name)

	// Verify deep copy
	copied.Items[0].Name = modifiedValue
	assert.NotEqual(t, original.Items[0].Name, copied.Items[0].Name)
}

func TestFluxInstanceList_DeepCopy_Nil(t *testing.T) {
	t.Parallel()

	var original *fluxinstaller.FluxInstanceList

	copied := original.DeepCopy()

	assert.Nil(t, copied)
}

func TestFluxInstanceList_DeepCopyObject(t *testing.T) {
	t.Parallel()

	original := &fluxinstaller.FluxInstanceList{
		Items: []fluxinstaller.FluxInstance{
			{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		},
	}

	obj := original.DeepCopyObject()

	require.NotNil(t, obj)
	copied, ok := obj.(*fluxinstaller.FluxInstanceList)
	require.True(t, ok)
	assert.Len(t, copied.Items, 1)
}

func TestFluxInstanceSpec_DeepCopyInto(t *testing.T) {
	t.Parallel()

	original := fluxinstaller.FluxInstanceSpec{
		Distribution: fluxinstaller.Distribution{
			Version: "2.x",
		},
		Sync: &fluxinstaller.Sync{
			Name:     "test",
			Interval: &metav1.Duration{Duration: 5 * time.Minute},
		},
	}

	var copied fluxinstaller.FluxInstanceSpec
	original.DeepCopyInto(&copied)

	assert.Equal(t, original.Distribution.Version, copied.Distribution.Version)
	require.NotNil(t, copied.Sync)
	assert.Equal(t, original.Sync.Name, copied.Sync.Name)
	assert.NotNil(t, copied.Sync.Interval)
}

func TestFluxInstanceSpec_DeepCopyInto_NilSync(t *testing.T) {
	t.Parallel()

	original := fluxinstaller.FluxInstanceSpec{
		Distribution: fluxinstaller.Distribution{
			Version: "2.x",
		},
		Sync: nil,
	}

	var copied fluxinstaller.FluxInstanceSpec
	original.DeepCopyInto(&copied)

	assert.Nil(t, copied.Sync)
}

func TestSync_DeepCopyInto(t *testing.T) {
	t.Parallel()

	original := fluxinstaller.Sync{
		Name:       "test",
		Kind:       "OCIRepository",
		URL:        "oci://example.com",
		Ref:        "main",
		Path:       "./",
		PullSecret: "secret",
		Provider:   "generic",
		Interval:   &metav1.Duration{Duration: time.Minute},
	}

	var copied fluxinstaller.Sync
	original.DeepCopyInto(&copied)

	assert.Equal(t, original.Name, copied.Name)
	assert.Equal(t, original.URL, copied.URL)
	require.NotNil(t, copied.Interval)
	assert.Equal(t, original.Interval.Duration, copied.Interval.Duration)

	// Verify deep copy of interval
	copied.Interval.Duration = 2 * time.Minute
	assert.NotEqual(t, original.Interval.Duration, copied.Interval.Duration)
}

func TestSync_DeepCopyInto_NilInterval(t *testing.T) {
	t.Parallel()

	original := fluxinstaller.Sync{
		Name:     "test",
		Interval: nil,
	}

	var copied fluxinstaller.Sync
	original.DeepCopyInto(&copied)

	assert.Nil(t, copied.Interval)
}

func TestFluxInstanceStatus_DeepCopy(t *testing.T) {
	t.Parallel()

	original := &fluxinstaller.FluxInstanceStatus{
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
			{Type: "Healthy", Status: metav1.ConditionFalse},
		},
	}

	copied := original.DeepCopy()

	require.NotNil(t, copied)
	assert.Len(t, copied.Conditions, 2)
	assert.Equal(t, "Ready", copied.Conditions[0].Type)
	assert.Equal(t, "Healthy", copied.Conditions[1].Type)
}

func TestFluxInstanceStatus_DeepCopy_Nil(t *testing.T) {
	t.Parallel()

	var original *fluxinstaller.FluxInstanceStatus

	copied := original.DeepCopy()

	assert.Nil(t, copied)
}

func TestFluxInstanceStatus_DeepCopy_NilConditions(t *testing.T) {
	t.Parallel()

	original := &fluxinstaller.FluxInstanceStatus{
		Conditions: nil,
	}

	copied := original.DeepCopy()

	require.NotNil(t, copied)
	assert.Nil(t, copied.Conditions)
}
