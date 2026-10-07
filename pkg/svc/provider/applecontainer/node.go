package applecontainer

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
)

// resourceNamePattern is what a node, cluster or volume name must match. It rules out a leading
// dash, which the CLI would read as a flag, and anything a label value cannot round-trip.
var resourceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// VolumeMount describes a named volume created for a node and mounted into it.
type VolumeMount struct {
	// Name is the volume's suffix; the volume itself is named "<node>-<name>".
	Name string
	// Destination is the absolute path inside the container.
	Destination string
	// Size is the volume size as the CLI accepts it (for example "10G"). Empty uses the
	// runtime's default.
	Size string
}

// NodeSpec describes one node container to create.
type NodeSpec struct {
	// Name is the container name and must be unique on the host.
	Name string
	// ClusterName is the cluster the node belongs to.
	ClusterName string
	// Role is RoleControlPlane or RoleWorker.
	Role string
	// Image is the container image reference.
	Image string
	// CPUs is the number of CPUs to allocate. Zero uses the runtime's default.
	CPUs int
	// MemoryMiB is the memory to allocate in MiB. Zero uses the runtime's default.
	MemoryMiB int
	// Privileged grants every capability and lifts the runtime's read-only and masked path
	// defaults. The runtime has no single privileged switch.
	Privileged bool
	// ReadOnlyRootFS mounts the container's root filesystem read-only.
	ReadOnlyRootFS bool
	// Env holds environment variables for the container's init process.
	Env map[string]string
	// Tmpfs lists absolute paths to mount as tmpfs.
	Tmpfs []string
	// Volumes lists the named volumes to create and mount.
	Volumes []VolumeMount
}

// volumeName returns the host-wide name of one of the node's volumes.
func (s NodeSpec) volumeName(mount VolumeMount) string {
	return s.Name + "-" + mount.Name
}

// validate rejects a specification the CLI would misread or that could not be found again.
func (s NodeSpec) validate() error {
	if !resourceNamePattern.MatchString(s.Name) {
		return fmt.Errorf("%w: name %q", ErrInvalidNodeSpec, s.Name)
	}

	if !resourceNamePattern.MatchString(s.ClusterName) {
		return fmt.Errorf("%w: cluster name %q", ErrInvalidNodeSpec, s.ClusterName)
	}

	if s.Role != RoleControlPlane && s.Role != RoleWorker {
		return fmt.Errorf("%w: role %q", ErrInvalidNodeSpec, s.Role)
	}

	if s.Image == "" || s.Image[0] == '-' {
		return fmt.Errorf("%w: image %q", ErrInvalidNodeSpec, s.Image)
	}

	return s.validateResources()
}

// validateResources rejects negative resource requests and volumes that could not be mounted or
// found again.
func (s NodeSpec) validateResources() error {
	if s.CPUs < 0 || s.MemoryMiB < 0 {
		return fmt.Errorf("%w: negative resources", ErrInvalidNodeSpec)
	}

	for _, mount := range s.Volumes {
		if !resourceNamePattern.MatchString(mount.Name) || mount.Destination == "" {
			return fmt.Errorf("%w: volume %q", ErrInvalidNodeSpec, mount.Name)
		}
	}

	return nil
}

// labelArgs returns the label flags that mark a container or volume as part of the node's cluster.
func (s NodeSpec) labelArgs() []string {
	return []string{
		"--label", LabelManagedBy + "=" + ManagedByValue,
		"--label", LabelCluster + "=" + s.ClusterName,
		"--label", LabelRole + "=" + s.Role,
	}
}

// runArgs returns the `container run` arguments for the node.
func (s NodeSpec) runArgs() []string {
	args := []string{"run", "--detach", "--name", s.Name}
	args = append(args, s.labelArgs()...)

	if s.Privileged {
		args = append(args,
			"--cap-add", "ALL", "--read-only-path", "NONE", "--masked-path", "NONE")
	}

	if s.ReadOnlyRootFS {
		args = append(args, "--read-only")
	}

	if s.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(s.CPUs))
	}

	if s.MemoryMiB > 0 {
		args = append(args, "--memory", strconv.Itoa(s.MemoryMiB)+"M")
	}

	keys := make([]string, 0, len(s.Env))
	for key := range s.Env {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, key := range keys {
		args = append(args, "--env", key+"="+s.Env[key])
	}

	for _, path := range s.Tmpfs {
		args = append(args, "--tmpfs", path)
	}

	for _, mount := range s.Volumes {
		args = append(args, "--volume", s.volumeName(mount)+":"+mount.Destination)
	}

	return append(args, s.Image)
}

// CreateNode creates the node's volumes and starts its container, all labelled so that ListNodes
// and DeleteNodes find them by cluster name. If the container cannot be started, the volumes this
// call created are removed again.
func (p *Provider) CreateNode(ctx context.Context, spec NodeSpec) error {
	if p.runner == nil {
		return fmt.Errorf("create node: %w", provider.ErrProviderUnavailable)
	}

	err := spec.validate()
	if err != nil {
		return err
	}

	// Refuse a name that is already taken, whoever owns it: the failure cleanup below removes the
	// container by name and must never reach one this call did not create.
	existing, err := p.listContainers(ctx)
	if err != nil {
		return err
	}

	for _, entry := range existing {
		if entry.ID == spec.Name {
			return fmt.Errorf("%w: %s", ErrNodeExists, spec.Name)
		}
	}

	created := make([]string, 0, len(spec.Volumes))

	for _, mount := range spec.Volumes {
		name := spec.volumeName(mount)

		args := append([]string{"volume", "create"}, spec.labelArgs()...)
		if mount.Size != "" {
			args = append(args, "-s", mount.Size)
		}

		_, err = p.runner.Run(ctx, append(args, name)...)
		if err != nil {
			p.cleanUpFailedNode(ctx, "", created)

			return fmt.Errorf("failed to create volume %s: %w", name, err)
		}

		created = append(created, name)
	}

	_, err = p.runner.Run(ctx, spec.runArgs()...)
	if err != nil {
		p.cleanUpFailedNode(ctx, spec.Name, created)

		return fmt.Errorf("failed to start node %s: %w", spec.Name, err)
	}

	return nil
}

// cleanUpFailedNode is the best-effort removal of what a failed CreateNode left behind: the
// container, when the runtime registered one before failing (it would keep the volumes in use),
// and then the volumes this call created. It uses a context that survives the caller's
// cancellation, since cancellation is a common reason to be cleaning up at all.
func (p *Provider) cleanUpFailedNode(ctx context.Context, container string, volumes []string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), availabilityTimeout)
	defer cancel()

	if container != "" {
		_, _ = p.runner.Run(cleanupCtx, "delete", "--force", "--", container)
	}

	if len(volumes) > 0 {
		_, _ = p.runner.Run(
			cleanupCtx, append([]string{"volume", "delete", "--"}, volumes...)...,
		)
	}
}
