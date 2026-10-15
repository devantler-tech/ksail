package eksinventory

import "context"

// QueryAWSForTest exposes the executable boundary to external package tests.
func QueryAWSForTest(ctx context.Context, args ...string) ([]byte, error) {
	return awsQuery(ctx, args...)
}
