package tenant

import "errors"

var (
	// ErrInvalidTenantType is returned when an invalid tenant type is provided.
	ErrInvalidTenantType = errors.New("invalid tenant type")
	// ErrTenantNameRequired is returned when the tenant name is empty.
	ErrTenantNameRequired = errors.New("tenant name is required")
	// ErrTenantTypeRequired is returned when the tenant type is not specified and cannot be auto-detected.
	ErrTenantTypeRequired = errors.New("tenant type is required (use --type flag or configure gitOpsEngine in ksail.yaml)")
	// ErrKustomizationNotFound is returned when no kustomization.yaml is found.
	ErrKustomizationNotFound = errors.New("kustomization.yaml not found")
)
