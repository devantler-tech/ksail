package gen

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	cmdhelpers "github.com/devantler-tech/ksail-go/pkg/cmd"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	yamlgenerator "github.com/devantler-tech/ksail-go/pkg/io/generator/yaml"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	defaultInterval      = 1 * time.Minute
	defaultTimeout       = 5 * time.Minute
	kindNameSeparator    = 2
	namespaceSeparator   = 2
	dependencyParts      = 2
	singleDependencyPart = 1
)

const helmReleaseExamples = `  # Generate a HelmRelease with a chart from a HelmRepository source
  ksail workload gen helmrelease podinfo \
    --interval=10m \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --chart-version=">4.0.0" \
    --export

  # Generate a HelmRelease with a chart from a GitRepository source
  ksail workload gen helmrelease podinfo \
    --interval=10m \
    --source=GitRepository/podinfo \
    --chart=./charts/podinfo \
    --export

  # Generate a HelmRelease with values from local YAML files
  ksail workload gen helmrelease podinfo \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --values=./my-values1.yaml \
    --values=./my-values2.yaml \
    --export

  # Generate a HelmRelease with values from a Kubernetes secret
  ksail workload gen helmrelease podinfo \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --values-from=Secret/my-secret-values \
    --export

  # Generate a HelmRelease with a custom release name
  ksail workload gen helmrelease podinfo \
    --release-name=podinfo-dev \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --export

  # Generate a HelmRelease targeting another namespace
  ksail workload gen helmrelease podinfo \
    --target-namespace=test \
    --create-target-namespace=true \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --export

  # Generate a HelmRelease using a chart reference
  ksail workload gen helmrelease podinfo \
    --namespace=default \
    --chart-ref=HelmChart/podinfo.flux-system \
    --export`

var (
	errInvalidFormat     = errors.New("invalid format")
	errInvalidKind       = errors.New("invalid kind")
	errInvalidDependency = errors.New("invalid depends-on format")
	errNotImplemented    = errors.New(
		"applying HelmRelease to cluster is not yet implemented, use --export flag to generate YAML",
	)
	errMissingSourceOrRef = errors.New(
		"either --source with --chart or --chart-ref must be specified",
	)
	errConflictingSource = errors.New("cannot specify both --source/--chart and --chart-ref")
)

// resourceReference represents a parsed Kubernetes resource reference.
type resourceReference struct {
	Kind      string
	Name      string
	Namespace string
}

// parseResourceReference parses a string in format "Kind/name" or "Kind/name.namespace".
func parseResourceReference(
	ref, defaultNamespace, errorContext string,
) (*resourceReference, error) {
	parts := strings.Split(ref, "/")
	if len(parts) != kindNameSeparator {
		return nil, fmt.Errorf(
			"%w: %s, expected Kind/name or Kind/name.namespace",
			errInvalidFormat,
			errorContext,
		)
	}

	resRef := &resourceReference{
		Kind:      parts[0],
		Name:      parts[1],
		Namespace: defaultNamespace,
	}

	// Check if namespace is included in the name
	if strings.Contains(resRef.Name, ".") {
		nameParts := strings.SplitN(resRef.Name, ".", namespaceSeparator)
		resRef.Name = nameParts[0]
		resRef.Namespace = nameParts[1]
	}

	return resRef, nil
}

// validateKind checks if a kind is in the list of valid kinds.
func validateKind(kind string, validKinds []string, errorContext string) error {
	if slices.Contains(validKinds, kind) {
		return nil
	}

	return fmt.Errorf(
		"%w: %s kind %q, must be one of: %s",
		errInvalidKind,
		errorContext,
		kind,
		strings.Join(validKinds, ", "),
	)
}

// validateKindCaseInsensitive checks if a kind matches (case-insensitive)
// one of the valid kinds and returns the canonical form.
func validateKindCaseInsensitive(
	kind string,
	validKinds []string,
	errorContext string,
) (string, error) {
	for _, validKind := range validKinds {
		if strings.EqualFold(kind, validKind) {
			return validKind, nil
		}
	}

	return "", fmt.Errorf(
		"%w: %s kind %q, must be one of: %s",
		errInvalidKind,
		errorContext,
		kind,
		strings.Join(validKinds, ", "),
	)
}

// parseDependency parses a depends-on reference in format "name" or "namespace/name".
func parseDependency(dep string) (*helmv2.DependencyReference, error) {
	parts := strings.Split(dep, "/")
	if len(parts) == singleDependencyPart {
		// Same namespace
		return &helmv2.DependencyReference{
			Name: parts[0],
		}, nil
	}

	if len(parts) == dependencyParts {
		// Different namespace
		return &helmv2.DependencyReference{
			Namespace: parts[0],
			Name:      parts[1],
		}, nil
	}

	return nil, fmt.Errorf("%w: %q, expected name or namespace/name", errInvalidDependency, dep)
}

// NewHelmReleaseCmd creates the workload gen helmrelease command.
func NewHelmReleaseCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "helmrelease [NAME]",
		Aliases: []string{"hr"},
		Short:   "Generate a HelmRelease resource",
		Long: "Generate a HelmRelease resource for a given HelmRepository, " +
			"GitRepository, Bucket, or chart reference source.",
		Example:      helmReleaseExamples,
		Args:         cobra.ExactArgs(1),
		RunE:         runHelmReleaseGen,
		SilenceUsage: true,
	}

	configureHelmReleaseFlags(cmd)

	return cmd
}

func runHelmReleaseGen(cmd *cobra.Command, args []string) error {
	tmr := timer.New()
	tmr.Start()

	cfg := readHelmReleaseFlags(cmd, args)

	err := validateHelmReleaseArgs(cfg.source, cfg.chart, cfg.chartRef, cfg.crdsPolicy)
	if err != nil {
		return err
	}

	helmRelease, err := buildHelmRelease(
		cfg.name,
		cfg.namespace,
		cfg.source,
		cfg.chart,
		cfg.chartVersion,
		cfg.chartRef,
		cfg.targetNamespace,
		cfg.storageNamespace,
		cfg.createNamespace,
		cfg.dependsOn,
		cfg.interval,
		cfg.timeout,
		cfg.values,
		cfg.valuesFrom,
		cfg.saName,
		cfg.crdsPolicy,
		cfg.kubeConfigSecretRef,
		cfg.releaseName,
	)
	if err != nil {
		return err
	}

	yaml, err := generateHelmReleaseYAML(helmRelease)
	if err != nil {
		return err
	}

	return outputHelmRelease(cmd, yaml, tmr)
}

func generateHelmReleaseYAML(helmRelease *helmv2.HelmRelease) (string, error) {
	generator := yamlgenerator.NewYAMLGenerator[helmv2.HelmRelease]()
	opts := yamlgenerator.Options{
		Output: "",
		Force:  false,
	}

	yaml, err := generator.Generate(*helmRelease, opts)
	if err != nil {
		return "", fmt.Errorf("failed to generate HelmRelease YAML: %w", err)
	}

	return yaml, nil
}

func outputHelmRelease(cmd *cobra.Command, yaml string, tmr timer.Timer) error {
	export, _ := cmd.Flags().GetBool("export")
	if export {
		_, err := fmt.Fprint(cmd.OutOrStdout(), yaml)
		if err != nil {
			return fmt.Errorf("failed to write YAML: %w", err)
		}
	} else {
		return errNotImplemented
	}

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "generated HelmRelease",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

func readHelmReleaseFlags(cmd *cobra.Command, args []string) helmReleaseConfig {
	name := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	source, _ := cmd.Flags().GetString("source")
	chart, _ := cmd.Flags().GetString("chart")
	chartVersion, _ := cmd.Flags().GetString("chart-version")
	chartRef, _ := cmd.Flags().GetString("chart-ref")
	targetNamespace, _ := cmd.Flags().GetString("target-namespace")
	storageNamespace, _ := cmd.Flags().GetString("storage-namespace")
	createNamespace, _ := cmd.Flags().GetBool("create-target-namespace")
	dependsOn, _ := cmd.Flags().GetStringSlice("depends-on")
	interval, _ := cmd.Flags().GetDuration("interval")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	values, _ := cmd.Flags().GetStringSlice("values")
	valuesFrom, _ := cmd.Flags().GetStringSlice("values-from")
	saName, _ := cmd.Flags().GetString("service-account")
	crdsPolicy, _ := cmd.Flags().GetString("crds")
	kubeConfigSecretRef, _ := cmd.Flags().GetString("kubeconfig-secret-ref")
	releaseName, _ := cmd.Flags().GetString("release-name")

	return newHelmReleaseConfig(name, namespace, source, chart, chartVersion, chartRef,
		targetNamespace, storageNamespace, createNamespace, dependsOn, interval, timeout,
		values, valuesFrom, saName, crdsPolicy, kubeConfigSecretRef, releaseName)
}

func newHelmReleaseConfig(name, namespace, source, chart, chartVersion, chartRef,
	targetNamespace, storageNamespace string, createNamespace bool, dependsOn []string,
	interval, timeout time.Duration, values, valuesFrom []string, saName, crdsPolicy,
	kubeConfigSecretRef, releaseName string,
) helmReleaseConfig {
	return helmReleaseConfig{
		name:                name,
		namespace:           namespace,
		source:              source,
		chart:               chart,
		chartVersion:        chartVersion,
		chartRef:            chartRef,
		targetNamespace:     targetNamespace,
		storageNamespace:    storageNamespace,
		createNamespace:     createNamespace,
		dependsOn:           dependsOn,
		interval:            interval,
		timeout:             timeout,
		values:              values,
		valuesFrom:          valuesFrom,
		saName:              saName,
		crdsPolicy:          crdsPolicy,
		kubeConfigSecretRef: kubeConfigSecretRef,
		releaseName:         releaseName,
	}
}

func configureHelmReleaseFlags(cmd *cobra.Command) {
	flags := cmd.Flags()

	// Required flags
	flags.String(
		"source",
		"",
		"source that contains the chart (HelmRepository/name, GitRepository/name, Bucket/name)",
	)
	flags.String("chart", "", "Helm chart name or path")
	flags.String("chart-ref", "", "Helm chart reference (HelmChart/name, OCIRepository/name)")

	// Optional flags
	flags.StringP("namespace", "n", "default", "namespace scope for the HelmRelease")
	flags.Duration("interval", defaultInterval, "reconciliation interval")
	flags.String("chart-version", "", "Helm chart version, accepts a semver range")
	flags.String("target-namespace", "", "namespace to target when performing operations")
	flags.String("storage-namespace", "", "namespace for Helm storage")
	flags.Bool("create-target-namespace", false, "create the target namespace if not present")
	flags.StringSlice("depends-on", nil, "HelmReleases that must be ready before this release")
	flags.Duration("timeout", defaultTimeout, "timeout for any individual Kubernetes operation")
	flags.StringSlice("values", nil, "local values YAML files")
	flags.StringSlice("values-from", nil, "values from ConfigMap or Secret")
	flags.String("service-account", "", "service account name to impersonate")
	flags.String("crds", "", "CRDs policy (Create, CreateReplace, Skip)")
	flags.String(
		"kubeconfig-secret-ref",
		"",
		"KubeConfig secret reference for remote reconciliation",
	)
	flags.String("release-name", "", "name used for the Helm release")
	flags.Bool("export", false, "export in YAML format to stdout")
}

func validateHelmReleaseArgs(source, chart, chartRef, crdsPolicy string) error {
	// Either source+chart or chartRef must be specified
	hasSource := source != "" && chart != ""
	hasChartRef := chartRef != ""

	if !hasSource && !hasChartRef {
		return errMissingSourceOrRef
	}

	if hasSource && hasChartRef {
		return errConflictingSource
	}

	// Validate CRDs policy if specified
	if crdsPolicy != "" {
		validPolicies := []string{"Create", "CreateReplace", "Skip"}

		err := validateKind(crdsPolicy, validPolicies, "crds policy")
		if err != nil {
			return err
		}
	}

	return nil
}

func buildHelmRelease(name, namespace, source, chart, chartVersion, chartRef,
	targetNamespace, storageNamespace string, createNamespace bool, dependsOn []string,
	interval, timeout time.Duration, values, valuesFrom []string, saName, crdsPolicy,
	kubeConfigSecretRef, releaseName string,
) (*helmv2.HelmRelease, error) {
	cfg := newHelmReleaseConfig(name, namespace, source, chart, chartVersion, chartRef,
		targetNamespace, storageNamespace, createNamespace, dependsOn, interval, timeout,
		values, valuesFrom, saName, crdsPolicy, kubeConfigSecretRef, releaseName)

	return buildHelmReleaseFromConfig(cfg)
}

func buildHelmReleaseFromConfig(cfg helmReleaseConfig) (*helmv2.HelmRelease, error) {
	helmRelease := createHelmReleaseBase(cfg)

	err := configureChartSource(helmRelease, cfg)
	if err != nil {
		return nil, err
	}

	configureOptionalFields(helmRelease, cfg)

	err = configureDependenciesAndValues(helmRelease, cfg)
	if err != nil {
		return nil, err
	}

	return helmRelease, nil
}

func createHelmReleaseBase(cfg helmReleaseConfig) *helmv2.HelmRelease {
	return &helmv2.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			APIVersion: helmv2.GroupVersion.String(),
			Kind:       helmv2.HelmReleaseKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.name,
			Namespace: cfg.namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval: metav1.Duration{Duration: cfg.interval},
		},
	}
}

func configureChartSource(helmRelease *helmv2.HelmRelease, cfg helmReleaseConfig) error {
	if cfg.source != "" && cfg.chart != "" {
		err := setChartSpec(helmRelease, cfg.source, cfg.chart, cfg.chartVersion, cfg.namespace)
		if err != nil {
			return err
		}
	} else if cfg.chartRef != "" {
		err := setChartRef(helmRelease, cfg.chartRef, cfg.namespace)
		if err != nil {
			return err
		}
	}

	return nil
}

func configureOptionalFields(helmRelease *helmv2.HelmRelease, cfg helmReleaseConfig) {
	if cfg.releaseName != "" {
		helmRelease.Spec.ReleaseName = cfg.releaseName
	}

	if cfg.targetNamespace != "" {
		helmRelease.Spec.TargetNamespace = cfg.targetNamespace
	}

	if cfg.storageNamespace != "" {
		helmRelease.Spec.StorageNamespace = cfg.storageNamespace
	}

	if cfg.createNamespace {
		if helmRelease.Spec.Install == nil {
			helmRelease.Spec.Install = &helmv2.Install{}
		}

		helmRelease.Spec.Install.CreateNamespace = true
	}

	if cfg.timeout > 0 {
		helmRelease.Spec.Timeout = &metav1.Duration{Duration: cfg.timeout}
	}

	if cfg.saName != "" {
		helmRelease.Spec.ServiceAccountName = cfg.saName
	}

	setCRDsPolicy(helmRelease, cfg.crdsPolicy)

	if cfg.kubeConfigSecretRef != "" {
		helmRelease.Spec.KubeConfig = &meta.KubeConfigReference{
			SecretRef: &meta.SecretKeyReference{
				Name: cfg.kubeConfigSecretRef,
			},
		}
	}
}

func configureDependenciesAndValues(helmRelease *helmv2.HelmRelease, cfg helmReleaseConfig) error {
	err := setDependencies(helmRelease, cfg.dependsOn)
	if err != nil {
		return err
	}

	err = setValues(helmRelease, cfg.values, cfg.valuesFrom)
	if err != nil {
		return err
	}

	return nil
}

// mergeMaps merges two maps, with values from the second map taking precedence.
func mergeMaps(mapA, mapB map[string]any) map[string]any {
	out := make(map[string]any, len(mapA))

	maps.Copy(out, mapA)

	for key, val := range mapB {
		if valMap, ok := val.(map[string]any); ok {
			if bVal, ok := out[key]; ok {
				if bValMap, ok := bVal.(map[string]any); ok {
					out[key] = mergeMaps(bValMap, valMap)

					continue
				}
			}
		}

		out[key] = val
	}

	return out
}

// helmReleaseConfig holds all configuration for building a HelmRelease.
type helmReleaseConfig struct {
	name                string
	namespace           string
	source              string
	chart               string
	chartVersion        string
	chartRef            string
	targetNamespace     string
	storageNamespace    string
	createNamespace     bool
	dependsOn           []string
	interval            time.Duration
	timeout             time.Duration
	values              []string
	valuesFrom          []string
	saName              string
	crdsPolicy          string
	kubeConfigSecretRef string
	releaseName         string
}

// setChartSpec sets the chart specification for the HelmRelease.
func setChartSpec(
	helmRelease *helmv2.HelmRelease,
	source, chart, chartVersion, namespace string,
) error {
	sourceRef, err := parseResourceReference(source, namespace, "source")
	if err != nil {
		return err
	}

	// Validate source kind
	validSourceKinds := []string{
		sourcev1.HelmRepositoryKind,
		sourcev1.GitRepositoryKind,
		sourcev1.BucketKind,
	}

	err = validateKind(sourceRef.Kind, validSourceKinds, "source")
	if err != nil {
		return err
	}

	helmRelease.Spec.Chart = &helmv2.HelmChartTemplate{
		Spec: helmv2.HelmChartTemplateSpec{
			Chart: chart,
			SourceRef: helmv2.CrossNamespaceObjectReference{
				Kind:      sourceRef.Kind,
				Name:      sourceRef.Name,
				Namespace: sourceRef.Namespace,
			},
		},
	}

	if chartVersion != "" {
		helmRelease.Spec.Chart.Spec.Version = chartVersion
	}

	return nil
}

// setChartRef sets the chart reference for the HelmRelease.
func setChartRef(helmRelease *helmv2.HelmRelease, chartRef, namespace string) error {
	chartReference, err := parseResourceReference(chartRef, namespace, "chart-ref")
	if err != nil {
		return err
	}

	// Validate chartRef kind
	validChartRefKinds := []string{sourcev1.OCIRepositoryKind, sourcev1.HelmChartKind}

	err = validateKind(chartReference.Kind, validChartRefKinds, "chart-ref")
	if err != nil {
		return err
	}

	helmRelease.Spec.ChartRef = &helmv2.CrossNamespaceSourceReference{
		Kind:      chartReference.Kind,
		Name:      chartReference.Name,
		Namespace: chartReference.Namespace,
	}

	return nil
}

// setDependencies sets the dependencies for the HelmRelease.
func setDependencies(helmRelease *helmv2.HelmRelease, dependsOn []string) error {
	if len(dependsOn) == 0 {
		return nil
	}

	deps := []helmv2.DependencyReference{}

	for _, dep := range dependsOn {
		depRef, err := parseDependency(dep)
		if err != nil {
			return err
		}

		deps = append(deps, *depRef)
	}

	helmRelease.Spec.DependsOn = deps

	return nil
}

// setCRDsPolicy sets the CRDs policy for the HelmRelease.
func setCRDsPolicy(helmRelease *helmv2.HelmRelease, crdsPolicy string) {
	if crdsPolicy == "" {
		return
	}

	if helmRelease.Spec.Install == nil {
		helmRelease.Spec.Install = &helmv2.Install{}
	}

	helmRelease.Spec.Install.CRDs = helmv2.Create

	if helmRelease.Spec.Upgrade == nil {
		helmRelease.Spec.Upgrade = &helmv2.Upgrade{}
	}

	helmRelease.Spec.Upgrade.CRDs = helmv2.CRDsPolicy(crdsPolicy)
}

// setValues sets values from files and ConfigMaps/Secrets for the HelmRelease.
func setValues(helmRelease *helmv2.HelmRelease, values, valuesFrom []string) error {
	if len(values) > 0 {
		valuesMap, err := loadValuesFromFiles(values)
		if err != nil {
			return err
		}

		jsonData, err := json.Marshal(valuesMap)
		if err != nil {
			return fmt.Errorf("marshaling values to JSON: %w", err)
		}

		helmRelease.Spec.Values = &apiextensionsv1.JSON{Raw: jsonData}
	}

	if len(valuesFrom) > 0 {
		valuesRefs, err := parseValuesFrom(valuesFrom)
		if err != nil {
			return err
		}

		helmRelease.Spec.ValuesFrom = valuesRefs
	}

	return nil
}

// loadValuesFromFiles loads and merges values from multiple YAML files.
func loadValuesFromFiles(values []string) (map[string]any, error) {
	valuesMap := make(map[string]any)

	for _, vFile := range values {
		// #nosec G304 - file path is provided by user as intended
		data, err := os.ReadFile(vFile)
		if err != nil {
			return nil, fmt.Errorf("reading values file %s: %w", vFile, err)
		}

		jsonBytes, err := yaml.YAMLToJSON(data)
		if err != nil {
			return nil, fmt.Errorf("converting values to JSON from %s: %w", vFile, err)
		}

		jsonMap := make(map[string]any)

		err = json.Unmarshal(jsonBytes, &jsonMap)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling values from %s: %w", vFile, err)
		}

		valuesMap = mergeMaps(valuesMap, jsonMap)
	}

	return valuesMap, nil
}

// parseValuesFrom parses values-from references.
func parseValuesFrom(valuesFrom []string) ([]helmv2.ValuesReference, error) {
	valuesRefs := []helmv2.ValuesReference{}

	for _, vf := range valuesFrom {
		vfRef, err := parseResourceReference(vf, "", "values-from")
		if err != nil {
			return nil, err
		}

		// Validate values-from kind
		validKinds := []string{"ConfigMap", "Secret"}

		canonicalKind, err := validateKindCaseInsensitive(vfRef.Kind, validKinds, "values-from")
		if err != nil {
			return nil, err
		}

		valuesRefs = append(valuesRefs, helmv2.ValuesReference{
			Kind: canonicalKind,
			Name: vfRef.Name,
		})
	}

	return valuesRefs, nil
}
