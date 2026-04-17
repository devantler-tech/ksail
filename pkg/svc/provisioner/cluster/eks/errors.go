package eksprovisioner

import "errors"

// ErrClientRequired is returned when an eksctl client is not supplied.
var ErrClientRequired = errors.New("eksctl client is required")

// ErrConfigPathRequired is returned when no declarative eksctl.yaml path is
// supplied. The binary wrapper always accepts -f <config> rather than an
// inline spec, so the caller must point at a config file.
var ErrConfigPathRequired = errors.New("eksctl config path is required")
