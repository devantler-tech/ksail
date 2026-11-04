package gen

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const (
	receiverAPIVersion     = "notification.toolkit.fluxcd.io/v1"
	receiverKind           = "Receiver"
	receiverTypeGenericOID = "generic-oidc"
	defaultReceiverNS      = "flux-system"
)

const receiverExamples = `  # Generate a secret-less Receiver that GitHub Actions can call with its OIDC
  # token to reconcile a GitRepository, accepting only one organization's workflows
  ksail workload gen receiver github-actions \
    --type=generic-oidc \
    --oidc-issuer=https://token.actions.githubusercontent.com \
    --oidc-validation="claims.repository_owner == 'my-org'" \
    --oidc-validation-message="only my-org workflows may trigger a reconcile" \
    --resource=GitRepository/webapp

  # Reconcile only the OCIRepositories labelled for the pushed repository
  ksail workload gen receiver releases \
    --type=generic-oidc \
    --oidc-issuer=https://token.actions.githubusercontent.com \
    --oidc-validation="claims.repository_owner == 'my-org'" \
    --resource="OCIRepository/*=res.metadata.labels['repo'] == claims.repository"

  # Generate a GitHub webhook Receiver whose token is read from a Secret
  ksail workload gen receiver github-push \
    --type=github \
    --events=push \
    --secret-ref=webhook-token \
    --resource=GitRepository/webapp.apps`

var (
	errReceiverTypeRequired    = errors.New("--type is required")
	errInvalidReceiverType     = errors.New("invalid --type")
	errReceiverResourceMissing = errors.New("at least one --resource is required")
	errInvalidReceiverResource = errors.New("invalid --resource")
	errReceiverNeedsSecret     = errors.New("--secret-ref is required for this --type")
	errOIDCReceiverSecret      = errors.New(
		"--secret-ref is not used by --type=generic-oidc, which authenticates with the OIDC token",
	)
	errOIDCReceiverIssuer = errors.New(
		"--oidc-issuer (an http:// or https:// URL) is required for --type=generic-oidc",
	)
	errOIDCReceiverValidation = errors.New(
		"--type=generic-oidc needs at least one --oidc-validation that constrains the caller",
	)
	errOIDCFlagsNeedOIDCType = errors.New("--oidc-* flags require --type=generic-oidc")
	errOIDCMessageCount      = errors.New(
		"give one --oidc-validation-message per --oidc-validation, or none",
	)

	issuerURLPattern = regexp.MustCompile(`^https?://`)
)

// receiverTypes are the webhook sender types the Flux notification-controller accepts.
//
//nolint:gochecknoglobals // fixed allow-list mirrored from the Receiver CRD's spec.type enum.
var receiverTypes = []string{
	"generic", "generic-hmac", receiverTypeGenericOID, "github", "gitlab", "bitbucket",
	"harbor", "dockerhub", "quay", "gcr", "nexus", "acr", "cdevents",
}

// receiverResourceKinds are the kinds a Receiver can request a reconciliation for.
//
//nolint:gochecknoglobals // fixed allow-list mirrored from the Receiver CRD's resources[].kind enum.
var receiverResourceKinds = []string{
	"Bucket", "GitRepository", "Kustomization", "HelmRelease", "HelmChart", "HelmRepository",
	"ImageRepository", "ImagePolicy", "ImageUpdateAutomation", "OCIRepository",
	"ArtifactGenerator", "ExternalArtifact", "FluxInstance", "ResourceSet",
	"ResourceSetInputProvider",
}

type receiverManifest struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   receiverMetadata `json:"metadata"`
	Spec       receiverSpec     `json:"spec"`
}

type receiverMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type receiverSpec struct {
	Type           string                 `json:"type"`
	Events         []string               `json:"events,omitempty"`
	Resources      []receiverResource     `json:"resources"`
	ResourceFilter string                 `json:"resourceFilter,omitempty"`
	SecretRef      *receiverSecretRef     `json:"secretRef,omitempty"`
	OIDCProviders  []receiverOIDCProvider `json:"oidcProviders,omitempty"`
}

type receiverResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Filter    string `json:"filter,omitempty"`
}

type receiverSecretRef struct {
	Name string `json:"name"`
}

//nolint:tagliatelle // field names follow the Flux Receiver CRD.
type receiverOIDCProvider struct {
	IssuerURL   string                   `json:"issuerURL"`
	Audience    string                   `json:"audience,omitempty"`
	Validations []receiverOIDCValidation `json:"validations"`
}

type receiverOIDCValidation struct {
	Expression string `json:"expression"`
	Message    string `json:"message"`
}

type receiverConfig struct {
	name            string
	namespace       string
	receiverType    string
	events          []string
	resources       []string
	resourceFilter  string
	secretRef       string
	oidcIssuer      string
	oidcAudience    string
	oidcValidations []string
	oidcMessages    []string
}

// NewReceiverCmd creates the workload gen receiver command.
func NewReceiverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "receiver [NAME]",
		Short: "Generate a Flux Receiver resource",
		Long: "Generate a Flux notification-controller Receiver, the webhook endpoint that asks Flux " +
			"to reconcile the listed resources when an external system reports a change. " +
			"--type=generic-oidc needs no Secret: callers present an OIDC ID token that the " +
			"--oidc-validation expressions must accept.",
		Example:      receiverExamples,
		Args:         cobra.ExactArgs(1),
		RunE:         runReceiverGen,
		SilenceUsage: true,
	}

	flags := cmd.Flags()
	flags.StringP("namespace", "n", defaultReceiverNS, "namespace for the Receiver")
	flags.String("type", "", "webhook sender type ("+strings.Join(receiverTypes, ", ")+")")
	flags.StringSlice("events", nil, "event types to handle, e.g. push for GitHub")
	// StringArray, not StringSlice: CEL filters contain commas.
	flags.StringArray(
		"resource",
		nil,
		"resource to reconcile, repeatable (Kind/name[.namespace][=<CEL filter>]; name may be *)",
	)
	flags.String(
		"resource-filter",
		"",
		"CEL expression every resource must pass before it is reconciled",
	)
	flags.String(
		"secret-ref",
		"",
		"Secret holding the webhook token (every type except generic-oidc)",
	)
	flags.String("oidc-issuer", "", "OIDC issuer URL whose ID tokens are accepted (generic-oidc)")
	flags.String(
		"oidc-audience",
		"",
		"expected token audience (generic-oidc; Flux defaults to notification-controller)",
	)
	flags.StringArray(
		"oidc-validation",
		nil,
		"CEL expression over the token claims that must be true, repeatable (generic-oidc)",
	)
	flags.StringArray(
		"oidc-validation-message",
		nil,
		"message returned when the matching --oidc-validation is false, repeatable in the same order "+
			"(default: a generic refusal that does not reveal the expression)",
	)

	return cmd
}

func runReceiverGen(cmd *cobra.Command, args []string) error {
	cfg := receiverConfig{name: args[0]}
	cfg.namespace, _ = cmd.Flags().GetString("namespace")
	cfg.receiverType, _ = cmd.Flags().GetString("type")
	cfg.events, _ = cmd.Flags().GetStringSlice("events")
	cfg.resources, _ = cmd.Flags().GetStringArray("resource")
	cfg.resourceFilter, _ = cmd.Flags().GetString("resource-filter")
	cfg.secretRef, _ = cmd.Flags().GetString("secret-ref")
	cfg.oidcIssuer, _ = cmd.Flags().GetString("oidc-issuer")
	cfg.oidcAudience, _ = cmd.Flags().GetString("oidc-audience")
	cfg.oidcValidations, _ = cmd.Flags().GetStringArray("oidc-validation")
	cfg.oidcMessages, _ = cmd.Flags().GetStringArray("oidc-validation-message")

	manifest, err := buildReceiver(cfg)
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to generate Receiver YAML: %w", err)
	}

	_, err = cmd.OutOrStdout().Write(data)
	if err != nil {
		return fmt.Errorf("failed to write YAML: %w", err)
	}

	return nil
}

// buildReceiver validates the flags and assembles the Receiver.
func buildReceiver(cfg receiverConfig) (receiverManifest, error) {
	err := validateReceiverAuth(cfg)
	if err != nil {
		return receiverManifest{}, err
	}

	resources, err := parseReceiverResources(cfg.resources)
	if err != nil {
		return receiverManifest{}, err
	}

	spec := receiverSpec{
		Type:           cfg.receiverType,
		Events:         cfg.events,
		Resources:      resources,
		ResourceFilter: cfg.resourceFilter,
	}

	if cfg.receiverType == receiverTypeGenericOID {
		validations := make([]receiverOIDCValidation, 0, len(cfg.oidcValidations))
		for index, expression := range cfg.oidcValidations {
			message := fmt.Sprintf("the token does not satisfy validation %d", index+1)
			if len(cfg.oidcMessages) > 0 {
				message = cfg.oidcMessages[index]
			}

			validations = append(validations, receiverOIDCValidation{
				Expression: expression,
				Message:    message,
			})
		}

		spec.OIDCProviders = []receiverOIDCProvider{{
			IssuerURL:   cfg.oidcIssuer,
			Audience:    cfg.oidcAudience,
			Validations: validations,
		}}
	} else {
		spec.SecretRef = &receiverSecretRef{Name: cfg.secretRef}
	}

	return receiverManifest{
		APIVersion: receiverAPIVersion,
		Kind:       receiverKind,
		Metadata:   receiverMetadata{Name: cfg.name, Namespace: cfg.namespace},
		Spec:       spec,
	}, nil
}

// validateReceiverAuth checks the type and that exactly one authentication method fits it:
// a webhook token Secret, or an OIDC issuer with validations for generic-oidc.
func validateReceiverAuth(cfg receiverConfig) error {
	if cfg.receiverType == "" {
		return errReceiverTypeRequired
	}

	if !slices.Contains(receiverTypes, cfg.receiverType) {
		return fmt.Errorf("%w %q, must be one of: %s",
			errInvalidReceiverType, cfg.receiverType, strings.Join(receiverTypes, ", "))
	}

	if cfg.receiverType != receiverTypeGenericOID {
		return validateTokenReceiver(cfg)
	}

	return validateOIDCReceiver(cfg)
}

// validateTokenReceiver checks a Receiver that authenticates with a webhook token Secret.
func validateTokenReceiver(cfg receiverConfig) error {
	if cfg.oidcIssuer != "" || cfg.oidcAudience != "" || len(cfg.oidcValidations) > 0 ||
		len(cfg.oidcMessages) > 0 {
		return errOIDCFlagsNeedOIDCType
	}

	if cfg.secretRef == "" {
		return fmt.Errorf("%w %q", errReceiverNeedsSecret, cfg.receiverType)
	}

	return nil
}

// validateOIDCReceiver checks a generic-oidc Receiver: an issuer URL, at least one validation
// and no Secret, since the caller authenticates with its OIDC ID token.
func validateOIDCReceiver(cfg receiverConfig) error {
	switch {
	case cfg.secretRef != "":
		return errOIDCReceiverSecret
	case !issuerURLPattern.MatchString(cfg.oidcIssuer):
		return errOIDCReceiverIssuer
	case !slices.ContainsFunc(cfg.oidcValidations, func(v string) bool {
		return strings.TrimSpace(v) != ""
	}):
		return errOIDCReceiverValidation
	case len(cfg.oidcMessages) > 0 && len(cfg.oidcMessages) != len(cfg.oidcValidations):
		return fmt.Errorf("%w: %d messages for %d validations",
			errOIDCMessageCount, len(cfg.oidcMessages), len(cfg.oidcValidations))
	}

	return nil
}

// parseReceiverResources reads repeated "Kind/name[.namespace][=<CEL filter>]" values.
func parseReceiverResources(values []string) ([]receiverResource, error) {
	if len(values) == 0 {
		return nil, errReceiverResourceMissing
	}

	resources := make([]receiverResource, 0, len(values))

	for _, value := range values {
		reference, filter, _ := strings.Cut(value, "=")

		ref, err := parseResourceReference(strings.TrimSpace(reference), "", "resource")
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidReceiverResource, value, err)
		}

		if ref.Name == "" {
			return nil, fmt.Errorf("%w %q: the name is empty", errInvalidReceiverResource, value)
		}

		err = validateKind(ref.Kind, receiverResourceKinds, "resource")
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidReceiverResource, value, err)
		}

		resources = append(resources, receiverResource{
			Kind:      ref.Kind,
			Name:      ref.Name,
			Namespace: ref.Namespace,
			Filter:    strings.TrimSpace(filter),
		})
	}

	return resources, nil
}
