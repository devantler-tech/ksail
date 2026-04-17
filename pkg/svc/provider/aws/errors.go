package aws

import "errors"

// ErrClientRequired is returned when a Provider is constructed without an
// eksctl client. NewProvider rejects nil clients to keep the zero value of
// Provider safe (all methods return ErrProviderUnavailable).
var ErrClientRequired = errors.New("aws provider: eksctl client is required")
