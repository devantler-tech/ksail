//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package flux

import "sigs.k8s.io/controller-runtime/pkg/client"

// CopySpec exports copySpec for benchmarking.
var CopySpec = copySpec

// CopySpecFunc is the function signature for copySpec, exported for use in external test packages.
type CopySpecFunc = func(src, dst client.Object) error
