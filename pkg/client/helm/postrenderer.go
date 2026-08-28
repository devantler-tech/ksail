package helm

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	helmpostrenderer "helm.sh/helm/v4/pkg/postrenderer"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"sigs.k8s.io/yaml"
)

var (
	yamlDocumentSeparator = regexp.MustCompile(`(?m)^---[\t ]*\r?\n`)
	apiVersionScalarLine  = regexp.MustCompile(
		`(?m)^(apiVersion:[\t ]*)([^\t #\r\n]+)([\t ]*(?:#[^\r\n]*)?)(\r?)$`,
	)
)

var (
	errAPIVersionMigrationRequired = errors.New(
		"API version migration requires kind, source, and target",
	)
	errNoServedAPIVersion               = errors.New("no served API version")
	errRenderedAPIVersionNotReplaceable = errors.New(
		"rendered resource has no replaceable API version",
	)
)

type apiVersionMigrationKey struct {
	apiVersion string
	kind       string
}

type apiVersionPostRenderer struct {
	migrations map[apiVersionMigrationKey]string
}

type manifestIdentity struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

func optionalAPIVersionPostRenderer(
	renderer *apiVersionPostRenderer,
) helmpostrenderer.PostRenderer {
	if renderer == nil {
		return nil
	}

	return renderer
}

func (c *Client) newAPIVersionPostRenderer(
	migrations []APIVersionMigration,
) (*apiVersionPostRenderer, error) {
	discoveryClient, err := c.settings.RESTClientGetter().ToDiscoveryClient()
	if err != nil {
		return nil, fmt.Errorf("get discovery client for API version migration: %w", err)
	}

	resolved, err := resolveFreshAPIVersionMigrations(discoveryClient, migrations)
	if err != nil {
		return nil, err
	}

	return &apiVersionPostRenderer{migrations: resolved}, nil
}

func resolveFreshAPIVersionMigrations(
	discoveryClient discovery.CachedDiscoveryInterface,
	migrations []APIVersionMigration,
) (map[apiVersionMigrationKey]string, error) {
	discoveryClient.Invalidate()

	return resolveAPIVersionMigrations(discoveryClient, migrations)
}

func resolveAPIVersionMigrations(
	discoveryClient discovery.DiscoveryInterface,
	migrations []APIVersionMigration,
) (map[apiVersionMigrationKey]string, error) {
	resolved := make(map[apiVersionMigrationKey]string)

	for _, migration := range migrations {
		if migration.Kind == "" || migration.From == "" || migration.To == "" {
			return nil, errAPIVersionMigrationRequired
		}

		fromServed, err := apiResourceServed(discoveryClient, migration.From, migration.Kind)
		if err != nil {
			return nil, fmt.Errorf(
				"discover source API %s kind %s: %w",
				migration.From,
				migration.Kind,
				err,
			)
		}

		if fromServed {
			continue
		}

		toServed, err := apiResourceServed(discoveryClient, migration.To, migration.Kind)
		if err != nil {
			return nil, fmt.Errorf(
				"discover target API %s kind %s: %w",
				migration.To,
				migration.Kind,
				err,
			)
		}

		if !toServed {
			return nil, fmt.Errorf(
				"%w for kind %s (checked %s and %s)",
				errNoServedAPIVersion,
				migration.Kind,
				migration.From,
				migration.To,
			)
		}

		key := apiVersionMigrationKey{apiVersion: migration.From, kind: migration.Kind}
		resolved[key] = migration.To
	}

	return resolved, nil
}

func apiResourceServed(
	discoveryClient discovery.DiscoveryInterface,
	apiVersion, kind string,
) (bool, error) {
	resources, err := discoveryClient.ServerResourcesForGroupVersion(apiVersion)
	if err != nil {
		if apierrors.IsNotFound(err) || errors.Is(err, memory.ErrCacheNotFound) {
			return false, nil
		}

		return false, fmt.Errorf("discover resources for API version %s: %w", apiVersion, err)
	}

	for _, resource := range resources.APIResources {
		if resource.Kind == kind {
			return true, nil
		}
	}

	return false, nil
}

func (r *apiVersionPostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
	if len(r.migrations) == 0 {
		return renderedManifests, nil
	}

	manifest := renderedManifests.Bytes()
	separators := yamlDocumentSeparator.FindAllIndex(manifest, -1)
	output := bytes.NewBuffer(make([]byte, 0, len(manifest)))
	start := 0

	for _, separator := range separators {
		document, err := r.renderDocument(manifest[start:separator[0]])
		if err != nil {
			return nil, err
		}

		_, _ = output.Write(document)
		_, _ = output.Write(manifest[separator[0]:separator[1]])
		start = separator[1]
	}

	document, err := r.renderDocument(manifest[start:])
	if err != nil {
		return nil, err
	}

	_, _ = output.Write(document)

	return output, nil
}

func (r *apiVersionPostRenderer) renderDocument(document []byte) ([]byte, error) {
	identity := manifestIdentity{}

	unmarshalErr := yaml.Unmarshal(document, &identity)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("read rendered manifest identity: %w", unmarshalErr)
	}

	target, ok := r.migrations[apiVersionMigrationKey{
		apiVersion: identity.APIVersion,
		kind:       identity.Kind,
	}]
	if !ok {
		return document, nil
	}

	output, replaced := replaceRenderedAPIVersion(document, identity.APIVersion, target)
	if !replaced {
		return nil, fmt.Errorf(
			"%w: kind %s, apiVersion %s",
			errRenderedAPIVersionNotReplaceable,
			identity.Kind,
			identity.APIVersion,
		)
	}

	return output, nil
}

func replaceRenderedAPIVersion(document []byte, current, target string) ([]byte, bool) {
	match := apiVersionScalarLine.FindSubmatchIndex(document)
	if match == nil {
		return nil, false
	}

	scalar, quote := unquoteAPIVersionScalar(document[match[4]:match[5]])
	if string(scalar) != current {
		return nil, false
	}

	replacement := []byte(target)
	if quote != 0 {
		replacement = append([]byte{quote}, replacement...)
		replacement = append(replacement, quote)
	}

	output := make([]byte, 0, len(document)+len(replacement)-len(scalar))
	output = append(output, document[:match[4]]...)
	output = append(output, replacement...)
	output = append(output, document[match[5]:]...)

	return output, true
}

func unquoteAPIVersionScalar(scalar []byte) ([]byte, byte) {
	if len(scalar) >= 2 &&
		((scalar[0] == '"' && scalar[len(scalar)-1] == '"') ||
			(scalar[0] == '\'' && scalar[len(scalar)-1] == '\'')) {
		return scalar[1 : len(scalar)-1], scalar[0]
	}

	return scalar, 0
}
