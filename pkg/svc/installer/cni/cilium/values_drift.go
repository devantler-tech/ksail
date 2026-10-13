package ciliuminstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
)

// RenderedValues returns the user-supplied values Install sends to Helm for the
// Cilium chart, as Helm stores them on the release.
func (c *Installer) RenderedValues() (map[string]any, error) {
	base, err := c.valuesBase()
	if err != nil {
		return nil, err
	}

	//nolint:wrapcheck // helmutil already wraps with the component name
	return base.RenderedValues()
}

// ValuesDrifted reports whether the installed Cilium release carries other
// user-supplied values than the ones this installer renders (ksail#7722).
//
// [helmutil.Base.ValuesDrifted] reads release ownership from the same storage
// labels Install checks, so a release Install would skip (owned by Flux or
// Argo CD) never reports a reconcile that would not happen. A missing release
// is not drift either: installing it is the cluster.cni field's job.
func (c *Installer) ValuesDrifted(ctx context.Context) (bool, error) {
	base, err := c.valuesBase()
	if err != nil {
		return false, err
	}

	//nolint:wrapcheck // helmutil already wraps with the component name
	return base.ValuesDrifted(ctx)
}

// valuesBase wraps the chart spec Install uses, so the comparison reads the
// values of exactly the release Install would upgrade.
func (c *Installer) valuesBase() (*helmutil.Base, error) {
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("cilium values client: %w", err)
	}

	spec := c.chartSpec()

	return helmutil.NewBase(spec.ReleaseName, client, c.GetTimeout(), nil, spec), nil
}
