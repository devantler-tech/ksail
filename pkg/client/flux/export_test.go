//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package flux

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CopySpec exports copySpec for benchmarking.
var CopySpec = copySpec

// CopySpecFunc is the function signature for copySpec, exported for use in external test packages.
type CopySpecFunc = func(src, dst client.Object) error

// CheckHelmReleaseStuck exports checkHelmReleaseStuck for testing.
var CheckHelmReleaseStuck = checkHelmReleaseStuck

// CheckHelmReleaseStuckFunc is the function signature for checkHelmReleaseStuck.
type CheckHelmReleaseStuckFunc = func(hr *unstructured.Unstructured) *StuckHelmRelease

// IsConnectionError exports isConnectionError for testing.
var IsConnectionError = isConnectionError

// IsTransientAPIError exports isTransientAPIError for testing.
var IsTransientAPIError = isTransientAPIError

// TimeoutWaitingError exports timeoutWaitingError for testing.
var TimeoutWaitingError = timeoutWaitingError

// HandleTransientError exports handleTransientError for testing.
var HandleTransientError = handleTransientError

// IsPermanentOCIError exports isPermanentOCIError for testing.
var IsPermanentOCIError = isPermanentOCIError

// EvaluateOCIRepositoryConditions exports evaluateOCIRepositoryConditions for testing.
var EvaluateOCIRepositoryConditions = evaluateOCIRepositoryConditions

// OCITimeoutError exports ociTimeoutError for testing.
var OCITimeoutError = ociTimeoutError

// ReconcileRequestHandled exports reconcileRequestHandled for testing.
var ReconcileRequestHandled = reconcileRequestHandled
