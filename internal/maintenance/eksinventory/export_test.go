package eksinventory

import "context"

// QueryAWSForTest exposes the executable boundary to external package tests.
func QueryAWSForTest(ctx context.Context, args ...string) ([]byte, error) {
	return awsQuery(ctx, args...)
}

// FailureReasonForTest exposes the public-safe classification for boundary tests.
func FailureReasonForTest(err error) string { return failureReason(err) }
