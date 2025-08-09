package cluster

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// DNS-1123 label (Kubernetes style) for names (optional but common).
	nameRegex     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	maxNameLength = 63
	reservedNames = map[string]struct{}{"default": {}, "kubernetes": {}}
)

func ValidateCluster(c *Cluster) error {
	if c == nil {
		return errors.New("cluster is nil")
	}

	var problems []string

	name := strings.TrimSpace(c.Metadata.Name)
	if name == "" {
		problems = append(problems, "metadata.name: must not be empty")
	} else {
		if len(name) > maxNameLength {
			problems = append(problems, fmt.Sprintf("metadata.name: length %d exceeds %d", len(name), maxNameLength))
		}
		if !nameRegex.MatchString(name) {
			problems = append(problems, "metadata.name: must match regex "+nameRegex.String())
		}
		if _, reserved := reservedNames[name]; reserved {
			problems = append(problems, "metadata.name: is reserved")
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New("validation failed:\n  - " + strings.Join(problems, "\n  - "))
}

func (c *Cluster) IsValid() bool {
	return ValidateCluster(c) == nil
}
