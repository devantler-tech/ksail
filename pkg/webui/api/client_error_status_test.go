package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestClientErrorStatus pins the backend-error → HTTP-status contract that lets clients and the UI
// react to specific failures instead of treating everything as a 500. It covers both the
// ClusterService sentinels (local CLI backend) and the Kubernetes apierrors (operator backend), plus
// error wrapping and the catch-all default.
func TestClientErrorStatus(t *testing.T) {
	t.Parallel()

	resource := schema.GroupResource{Group: "ksail.dev", Resource: "clusters"}
	kind := schema.GroupKind{Group: "ksail.dev", Kind: "Cluster"}

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"sentinel not found", api.ErrNotFound, http.StatusNotFound},
		{
			"wrapped sentinel not found",
			fmt.Errorf("get cluster: %w", api.ErrNotFound),
			http.StatusNotFound,
		},
		{"apierror not found", apierrors.NewNotFound(resource, "dev"), http.StatusNotFound},
		{"sentinel already exists", api.ErrAlreadyExists, http.StatusConflict},
		{"apierror conflict", apierrors.NewConflict(resource, "dev", nil), http.StatusConflict},
		{
			"apierror already exists",
			apierrors.NewAlreadyExists(resource, "dev"),
			http.StatusConflict,
		},
		{"sentinel invalid", api.ErrInvalid, http.StatusUnprocessableEntity},
		{
			"apierror invalid",
			apierrors.NewInvalid(kind, "dev", nil),
			http.StatusUnprocessableEntity,
		},
		{"sentinel not supported", api.ErrNotSupported, http.StatusNotImplemented},
		{"apierror bad request", apierrors.NewBadRequest("bad"), http.StatusBadRequest},
		{"apierror unauthorized", apierrors.NewUnauthorized("nope"), http.StatusUnauthorized},
		{"apierror forbidden", apierrors.NewForbidden(resource, "dev", nil), http.StatusForbidden},
		{"unmapped error falls back to 500", errBoom, http.StatusInternalServerError},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, api.ClientErrorStatus(testCase.err))
		})
	}
}
