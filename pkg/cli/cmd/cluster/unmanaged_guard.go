package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	eksctlclient "github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
)

// ErrUnmanagedCluster indicates a ksail-only lifecycle action (start/stop/…) was attempted against a
// kubeconfig context ksail did not provision — an unmanaged cluster (a managed cloud cluster, a
// kubeadm cluster, a colleague's cluster). Read-only operations still work; only ksail-only
// lifecycle actions are refused (ksail#5885, part of the unmanaged-cluster surface epic #5654).
var ErrUnmanagedCluster = errors.New("cluster is not managed by ksail")

var (
	errAWSOwnershipNameMismatch = errors.New(
		"exact AWS ownership query returned a different cluster",
	)
	errAWSOwnershipRegionMismatch = errors.New(
		"exact AWS ownership query returned a different region",
	)
	errAWSOwnershipRegionMissing = errors.New(
		"exact AWS ownership query did not report a region",
	)
	errAWSOwnershipRegionAmbiguous = errors.New(
		"multiple kubeconfig regions match EKS cluster",
	)
	errAWSOwnershipProvenanceMissing = errors.New(
		"exact AWS ownership query did not report an eksctl-created cluster",
	)
)

type eksIdentityClientFactory func(
	ctx context.Context,
	region string,
	resolution credentials.AWSResolution,
) (eksidentity.Client, error)

//nolint:gochecknoglobals // synchronized injectable construction seam for offline lifecycle tests.
var eksIdentityClientFactoryState = struct {
	sync.RWMutex

	factory eksIdentityClientFactory
}{
	factory: func(
		ctx context.Context,
		region string,
		resolution credentials.AWSResolution,
	) (eksidentity.Client, error) {
		options := credentials.OptionsForFrozenAWSConfig(
			resolution,
			eksclient.WithAWSConfig,
			eksclient.WithCredentialValues,
			eksclient.RequireCredentialValues,
		)

		return eksclient.NewClient(ctx, region, options...)
	},
}

// managedClusterLister enumerates the names of clusters ksail manages across every provider and
// reports whether discovery was complete (no provider failed). The guard fails open when discovery
// is incomplete so a transient provider error (e.g. a stopped Docker daemon) never wrongly refuses a
// genuine managed cluster. Overridable in tests.
type managedClusterLister func(ctx context.Context) (managed map[string]struct{}, complete bool)

// discoverManagedClusters is the production managedClusterLister: it queries every provider via the
// shared clusterdiscovery.Discoverer — the same enumeration `ksail cluster list` uses — and keys the
// result by cluster name. complete is false when any provider failed to list, so the guard can fail
// open rather than refuse a cluster the failed provider might actually manage.
func discoverManagedClusters(ctx context.Context) (map[string]struct{}, bool) {
	clusters, failures := (&clusterdiscovery.Discoverer{}).
		Discover(ctx, clusterdiscovery.DefaultProviders())

	managed := make(map[string]struct{}, len(clusters))
	for _, cluster := range clusters {
		managed[cluster.Name] = struct{}{}
	}

	return managed, len(failures) == 0
}

// ensureClusterManaged rejects a ksail-only lifecycle action when the target cluster is NOT among
// ksail's managed clusters but a matching kubeconfig context DOES exist — i.e. the user is pointing
// ksail at a cluster it did not provision. It is best-effort and FAILS OPEN so a genuine managed
// cluster is never wrongly refused: it returns nil when discovery could not fully enumerate every
// provider (complete=false), or when the kubeconfig has no matching context (a nonexistent cluster,
// left to the normal not-found path). Only a resolved cluster that is unmanaged AND present in the
// kubeconfig is refused.
func ensureClusterManaged(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
	lister managedClusterLister,
) error {
	managed, complete := lister(ctx)
	if !complete {
		return nil
	}

	isManaged := func(name string) bool {
		_, ok := managed[name]

		return ok
	}

	if clusterdiscovery.ContextIsManaged(resolved.ClusterName, isManaged) {
		return nil
	}

	if !kubeconfigHasClusterContext(resolved.KubeconfigPath, resolved.ClusterName) {
		return nil
	}

	return fmt.Errorf(
		"%q is an unmanaged cluster: %w; read-only operations (list, resource browsing, logs, exec) still work",
		resolved.ClusterName,
		ErrUnmanagedCluster,
	)
}

// kubeconfigHasClusterContext reports whether the kubeconfig at kubeconfigPath contains a context
// that maps to clusterName — directly, or via ksail's context→name detection so a Docker cluster's
// "kind-dev" context matches the ksail name "dev". It reuses clusterdiscovery.ContextIsManaged so the
// context↔name mapping stays defined in exactly one place. Best-effort: a missing/unreadable
// kubeconfig yields false (treated as "not an unmanaged cluster, just absent").
func kubeconfigHasClusterContext(kubeconfigPath, clusterName string) bool {
	return kubeconfigHasResolvedClusterContext(&lifecycle.ResolvedClusterInfo{
		ClusterName:    clusterName,
		KubeconfigPath: kubeconfigPath,
	})
}

// kubeconfigHasResolvedClusterContext extends the shared prefix-based context mapping with the
// suffix-shaped eksctl context formats: <iam>@<name>.<region>.eksctl.io and
// <name>.<region>.eksctl.io.
func kubeconfigHasResolvedClusterContext(resolved *lifecycle.ResolvedClusterInfo) bool {
	config := clusterdiscovery.LoadKubeconfig(resolved.KubeconfigPath)
	if config == nil {
		return false
	}

	matchesClusterName := func(name string) bool { return name == resolved.ClusterName }

	for contextName := range config.Contexts {
		if resolved.Provider == v1alpha1.ProviderAWS {
			if eksctlContextMatchesCluster(
				contextName,
				resolved.ClusterName,
				resolved.AWSRegion,
			) {
				return true
			}

			continue
		}

		if clusterdiscovery.ContextIsManaged(contextName, matchesClusterName) {
			return true
		}
	}

	return false
}

// parseEksctlContextTarget recognizes both identity-qualified and bare eksctl kubeconfig contexts.
// Parsing from the last @ keeps IAM identities opaque; parsing the target at its last dot avoids
// prefix matches between different cluster names.
func parseEksctlContextTarget(contextName string) (string, string, bool) {
	target := contextName
	if identityEnd := strings.LastIndex(target, "@"); identityEnd >= 0 {
		target = target[identityEnd+1:]
	}

	target, found := strings.CutSuffix(target, ".eksctl.io")
	if !found {
		return "", "", false
	}

	regionStart := strings.LastIndex(target, ".")
	if regionStart <= 0 || regionStart == len(target)-1 {
		return "", "", false
	}

	clusterName := target[:regionStart]
	region := target[regionStart+1:]

	if strings.Contains(clusterName, ".") || strings.Contains(region, ".") {
		return "", "", false
	}

	return clusterName, region, true
}

// eksctlContextMatchesCluster recognizes eksctl's real kubeconfig context shape without treating a
// mere substring as ownership proof. When the resolved region is known it must match exactly.
func eksctlContextMatchesCluster(contextName, clusterName, region string) bool {
	contextCluster, contextRegion, ok := parseEksctlContextTarget(contextName)
	if !ok || contextCluster != clusterName {
		return false
	}

	return region == "" || contextRegion == region
}

// bindAWSRegionFromKubeconfig uses a context region only as a fallback when neither the configured
// environment variable nor eks.yaml supplied one. More than one exact-name region is ambiguous and
// must stop before even the read-only ownership query chooses an AWS profile default.
func bindAWSRegionFromKubeconfig(resolved *lifecycle.ResolvedClusterInfo) error {
	if resolved.AWSRegion != "" {
		return nil
	}

	config := clusterdiscovery.LoadKubeconfig(resolved.KubeconfigPath)
	if config == nil {
		return nil
	}

	regions := make(map[string]struct{})

	for contextName := range config.Contexts {
		clusterName, region, ok := parseEksctlContextTarget(contextName)
		if ok && clusterName == resolved.ClusterName {
			regions[region] = struct{}{}
		}
	}

	if len(regions) == 0 {
		return nil
	}

	if len(regions) == 1 {
		for region := range regions {
			resolved.AWSRegion = region
		}

		return nil
	}

	regionNames := make([]string, 0, len(regions))
	for region := range regions {
		regionNames = append(regionNames, region)
	}

	sort.Strings(regionNames)

	return fmt.Errorf(
		"%w %q (%s); set an explicit AWS region",
		errAWSOwnershipRegionAmbiguous,
		resolved.ClusterName,
		strings.Join(regionNames, ", "),
	)
}

// ensureAWSClusterManaged queries the exact name and region through eksctl under the resolved AWS
// credential aliases. Unlike broad discovery (which may silently skip an unavailable provider),
// every tool, credential, command, and parse error fails closed before a destructive operation.
func ensureAWSClusterManaged(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
) error {
	identityClient, err := resolveAWSOwnershipTarget(ctx, resolved)
	if err != nil {
		return err
	}

	clusterName := resolved.ClusterName
	region := resolved.AWSRegion

	verifier, err := eksidentity.NewVerifier(identityClient, clusterName, region)
	if err != nil {
		return fmt.Errorf("load immutable EKS ownership identity: %w", err)
	}

	err = verifier(ctx)
	if err != nil {
		return fmt.Errorf("verify immutable EKS ownership identity: %w", err)
	}

	resolved.AWSOwnershipVerifier = verifier

	return nil
}

// resolveAWSOwnershipTarget performs the legacy local-intent plus exact eksctl provenance check and
// returns an SDK client pinned to the same one-time credential snapshot. Normal mutations follow it
// with immutable identity verification; the explicit rebind flow deliberately uses only this
// read-only prerequisite before capturing a new identity.
func resolveAWSOwnershipTarget(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
) (eksidentity.Client, error) {
	if !hasLocalKSailEKSTargetEvidence(resolved) {
		return nil, fmt.Errorf(
			"no local KSail ownership evidence for EKS target %q: %w",
			resolved.ClusterName,
			unmanagedClusterError(resolved.ClusterName),
		)
	}

	err := bindAWSRegionFromKubeconfig(resolved)
	if err != nil {
		return nil, err
	}

	auth, err := queryFrozenAWSOwnership(ctx, resolved)
	if err != nil {
		return nil, err
	}

	resolved.AWSResolution = &auth

	eksIdentityClientFactoryState.RLock()
	factory := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.RUnlock()

	identityClient, err := factory(ctx, resolved.AWSRegion, auth)
	if err != nil {
		return nil, fmt.Errorf("create immutable EKS identity client: %w", err)
	}

	return identityClient, nil
}

func queryFrozenAWSOwnership(
	ctx context.Context,
	resolved *lifecycle.ResolvedClusterInfo,
) (credentials.AWSResolution, error) {
	auth, err := credentials.ResolveFrozenAWS(
		ctx,
		credentials.NewAWSOptionsResolver(resolved.AWSOpts),
		resolved.AWSRegion,
	)
	if err != nil {
		return credentials.AWSResolution{}, fmt.Errorf(
			"freeze AWS credentials for EKS ownership verification: %w",
			err,
		)
	}

	if resolved.AWSRegion == "" {
		resolved.AWSRegion = strings.TrimSpace(auth.Region)
	}

	eksctlOptions := credentials.OptionsForAWSChildEnvironment(
		auth,
		os.Environ(),
		eksctlclient.WithEnvironment,
		eksctlclient.RequireCredentialValues,
	)

	summary, err := eksctlclient.NewClient(eksctlOptions...).GetCluster(
		ctx,
		resolved.ClusterName,
		resolved.AWSRegion,
	)
	if err != nil {
		if errors.Is(err, eksctlclient.ErrClusterNotFound) {
			return credentials.AWSResolution{}, awsOwnershipMismatchError(resolved, err)
		}

		return credentials.AWSResolution{}, awsOwnershipQueryError(resolved, err)
	}

	err = validateAWSOwnershipSummary(resolved, summary)
	if err != nil {
		return credentials.AWSResolution{}, err
	}

	if auth.Region == "" {
		auth.Region = resolved.AWSRegion
	}

	return auth, nil
}

// hasLocalKSailEKSTargetEvidence requires local KSail intent before the cloud-side eksctl marker is
// consulted. Matching an actual loaded eks.yaml authorizes config-backed commands; persisted EKS/AWS
// creation state preserves standalone --name/--provider operation when project files are absent. A
// kubeconfig context or EksctlCreated=True alone never qualifies. These name-based records do not
// bind an immutable AWS account/cluster instance; ksail#6202 tracks that separate hardening.
func hasLocalKSailEKSTargetEvidence(resolved *lifecycle.ResolvedClusterInfo) bool {
	if resolved.EKSConfigSource && strings.TrimSpace(resolved.ConfigClusterName) != "" &&
		strings.TrimSpace(resolved.ConfigClusterName) == strings.TrimSpace(resolved.ClusterName) {
		return true
	}

	spec, err := state.LoadClusterSpec(resolved.ClusterName)
	if err != nil {
		return false
	}

	return spec.Distribution == v1alpha1.DistributionEKS && spec.Provider == v1alpha1.ProviderAWS
}

// validateAWSOwnershipSummary requires the exact query to corroborate name, region, and eksctl
// provenance. Every mismatch is a pre-mutation blocker.
func validateAWSOwnershipSummary(
	resolved *lifecycle.ResolvedClusterInfo,
	summary *eksctlclient.ClusterSummary,
) error {
	if summary.Name != resolved.ClusterName {
		return awsOwnershipMismatchError(
			resolved,
			fmt.Errorf(
				"%w: got %q, want %q",
				errAWSOwnershipNameMismatch,
				summary.Name,
				resolved.ClusterName,
			),
		)
	}

	if resolved.AWSRegion != "" && summary.Region != resolved.AWSRegion {
		return awsOwnershipMismatchError(
			resolved,
			fmt.Errorf(
				"%w: got %q, want %q",
				errAWSOwnershipRegionMismatch,
				summary.Region,
				resolved.AWSRegion,
			),
		)
	}

	if resolved.AWSRegion == "" && strings.TrimSpace(summary.Region) == "" {
		return awsOwnershipQueryError(resolved, errAWSOwnershipRegionMissing)
	}

	if !strings.EqualFold(strings.TrimSpace(summary.EKSCTLCreated), "true") {
		return fmt.Errorf(
			"AWS ownership query returned an unmanaged target: %w",
			errors.Join(
				fmt.Errorf(
					"%w: got EksctlCreated=%q",
					errAWSOwnershipProvenanceMissing,
					summary.EKSCTLCreated,
				),
				unmanagedClusterError(resolved.ClusterName),
			),
		)
	}

	if resolved.AWSRegion == "" {
		resolved.AWSRegion = summary.Region
	}

	return nil
}

// awsOwnershipMismatchError classifies an absent/mismatched exact result as unmanaged only when a
// strictly parsed eksctl context corroborates the requested name and region. A context identity is
// never treated as ownership proof.
func awsOwnershipMismatchError(resolved *lifecycle.ResolvedClusterInfo, cause error) error {
	if kubeconfigHasResolvedClusterContext(resolved) {
		return fmt.Errorf(
			"AWS ownership query did not return the kubeconfig target: %w",
			errors.Join(cause, unmanagedClusterError(resolved.ClusterName)),
		)
	}

	return awsOwnershipQueryError(resolved, cause)
}

// awsOwnershipQueryError preserves tool, credential, command, and parse failures as fail-closed
// diagnostics. Those failures do not prove a kubeconfig target is unmanaged; they only prove the
// destructive command cannot establish ownership safely.
func awsOwnershipQueryError(resolved *lifecycle.ResolvedClusterInfo, cause error) error {
	return fmt.Errorf(
		"verify AWS cluster ownership for %q: %w",
		resolved.ClusterName,
		cause,
	)
}

func unmanagedClusterError(clusterName string) error {
	return fmt.Errorf(
		"%q is an unmanaged cluster: %w; read-only operations (list, resource browsing, logs, exec) still work",
		clusterName,
		ErrUnmanagedCluster,
	)
}

// unmanagedClusterGuard is the SimpleLifecycleConfig.Guard shared by start and stop. AWS uses the
// exact fail-closed ownership query; other providers retain the existing cross-provider guard.
func unmanagedClusterGuard(ctx context.Context, resolved *lifecycle.ResolvedClusterInfo) error {
	if resolved.Provider == v1alpha1.ProviderAWS {
		return ensureAWSClusterManaged(ctx, resolved)
	}

	return ensureClusterManaged(ctx, resolved, discoverManagedClusters)
}

// updateUnmanagedGuardFunc is the unmanaged-cluster guard `cluster update` applies before it
// reconciles any configuration. It defaults to the real cross-provider guard; tests override it
// (via ExportSetUpdateUnmanagedGuard) so the refusal path is exercised without a live provider.
//
//nolint:gochecknoglobals // dependency injection for tests
var updateUnmanagedGuardFunc = unmanagedClusterGuard

// guardUpdateTargetManaged refuses `cluster update` when its target is a cluster ksail did not
// provision. Unlike delete/start/stop, `cluster update` has no --kubeconfig flag — it resolves its
// target from ksail.yaml — so the kubeconfig path is read from the loaded ClusterCfg and resolved
// the same way ResolveClusterInfo does, then handed to the shared unmanaged-cluster guard. The guard
// fails open on incomplete discovery, so a genuine managed cluster is never wrongly refused.
// (ksail#5885, epic #5654.)
func guardUpdateTargetManaged(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	eksConfig *clusterprovisioner.EKSConfig,
) (*credentials.AWSResolution, lifecycle.AWSOwnershipVerifier, error) {
	kubeconfigPath, err := clusterdetector.ResolveKubeconfigPath(
		clusterCfg.Spec.Cluster.Connection.Kubeconfig,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	// Canonicalize the user-supplied path before the guard reads the kubeconfig (repo path-safety
	// guideline; EvalCanonicalPath tolerates a not-yet-existing file by resolving its parent, so a
	// missing kubeconfig still falls through to the guard's fail-open path).
	canonical, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize kubeconfig path %q: %w", kubeconfigPath, err)
	}

	resolved := resolveUpdateTarget(clusterCfg, clusterName, canonical, eksConfig)

	err = lifecycle.ValidateStandaloneAWSTarget(resolved)
	if err != nil {
		return nil, nil, fmt.Errorf("validate AWS update target: %w", err)
	}

	err = updateUnmanagedGuardFunc(ctx, resolved)
	if err != nil {
		return nil, nil, err
	}

	// A regionless eks.yaml may be safely bound by an exact kubeconfig context or the exact eksctl
	// query. Preserve that verified region so the subsequent update/recreate cannot fall back to a
	// different ambient/default AWS region.
	if eksConfig != nil && strings.TrimSpace(resolved.AWSRegion) != "" {
		eksConfig.Region = strings.TrimSpace(resolved.AWSRegion)
	}

	return resolved.AWSResolution, resolved.AWSOwnershipVerifier, nil
}

func resolveUpdateTarget(
	clusterCfg *v1alpha1.Cluster,
	clusterName, canonicalKubeconfig string,
	eksConfig *clusterprovisioner.EKSConfig,
) *lifecycle.ResolvedClusterInfo {
	resolved := &lifecycle.ResolvedClusterInfo{
		ClusterName:    clusterName,
		Provider:       clusterCfg.Spec.Cluster.Provider,
		KubeconfigPath: canonicalKubeconfig,
		AWSOpts:        clusterCfg.Spec.Provider.AWS,
	}
	if eksConfig == nil {
		return resolved
	}

	configuredAWSRegion := strings.TrimSpace(eksConfig.Region)
	effectiveAWSRegion := lifecycle.ResolveAWSRegion(
		clusterCfg.Spec.Provider.AWS,
		&clusterprovisioner.DistributionConfig{EKS: eksConfig},
	)

	// The exact verified name/region drives both in-place operations and recreation deletion. Pin it
	// into the distribution config before either later path constructs its provisioner.
	eksConfig.Region = effectiveAWSRegion
	resolved.ConfigClusterName = strings.TrimSpace(eksConfig.Name)
	resolved.EKSConfigSource = eksConfig.NameFromConfig &&
		strings.TrimSpace(eksConfig.ConfigPath) != "" &&
		resolved.ConfigClusterName != ""
	resolved.AWSRegion = effectiveAWSRegion
	resolved.AWSRegionFromConfig = effectiveAWSRegion != "" &&
		effectiveAWSRegion == configuredAWSRegion

	return resolved
}
