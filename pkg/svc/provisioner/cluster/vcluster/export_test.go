//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package vclusterprovisioner

import "time"

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// VClusterReadyTimeoutForTest exposes vclusterReadyTimeout for unit testing.
var VClusterReadyTimeoutForTest = vclusterReadyTimeout

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry

// WaitForNetworkRemovalForTest exposes waitForNetworkRemoval for unit testing.
var WaitForNetworkRemovalForTest = waitForNetworkRemoval

// BuildValuesFilesForTest exposes buildValuesFiles for unit testing.
var BuildValuesFilesForTest = buildValuesFiles

// ResolvePersistenceDisabledForTest exposes resolvePersistenceDisabled for unit testing.
var ResolvePersistenceDisabledForTest = resolvePersistenceDisabled

// UserPersistenceIntentForTest exposes userPersistenceIntent for unit testing.
var UserPersistenceIntentForTest = userPersistenceIntent

// ErrPersistentStorageUnavailableForTest exposes errPersistentStorageUnavailable for unit testing.
var ErrPersistentStorageUnavailableForTest = errPersistentStorageUnavailable

// NetworkExistsFnForTest exposes the networkExistsFn type for unit testing.
type NetworkExistsFnForTest = networkExistsFn

// RemoveNetworkFnForTest exposes the removeNetworkFn type for unit testing.
type RemoveNetworkFnForTest = removeNetworkFn

// TestPollInterval is a short poll interval for unit tests to avoid slow test suites.
const TestPollInterval = time.Millisecond
