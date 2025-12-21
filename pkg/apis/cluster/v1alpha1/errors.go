package v1alpha1

import "errors"

// ErrInvalidDistribution is returned when an invalid distribution is specified.
var ErrInvalidDistribution = errors.New("invalid distribution")

// ErrInvalidGitOpsEngine is returned when an invalid GitOps engine is specified.
var ErrInvalidGitOpsEngine = errors.New("invalid GitOps engine")

// ErrInvalidCNI is returned when an invalid CNI is specified.
var ErrInvalidCNI = errors.New("invalid CNI")

// ErrInvalidCSI is returned when an invalid CSI is specified.
var ErrInvalidCSI = errors.New("invalid CSI")

// ErrInvalidMetricsServer is returned when an invalid metrics server is specified.
var ErrInvalidMetricsServer = errors.New("invalid metrics server")

// ErrInvalidCertManager is returned when an invalid cert-manager option is specified.
var ErrInvalidCertManager = errors.New("invalid cert-manager")

// ErrInvalidLocalRegistry is returned when an invalid local registry mode is specified.
var ErrInvalidLocalRegistry = errors.New("invalid local registry mode")

// ErrInvalidValidateWorkloadOnCreate is returned when an invalid validate workload on create option is specified.
var ErrInvalidValidateWorkloadOnCreate = errors.New("invalid validate workload on create")
