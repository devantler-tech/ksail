package helm

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/yaml"
)

var yamlDocumentSeparator = regexp.MustCompile(`(?m)^---[\t ]*\r?\n`)

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

func (c *Client) newAPIVersionPostRenderer(
	migrations []APIVersionMigration,
) (*apiVersionPostRenderer, error) {
	if len(migrations) == 0 {
		return nil, nil
	}

	discoveryClient, err := c.settings.RESTClientGetter().ToDiscoveryClient()
	if err != nil {
		return nil, fmt.Errorf("get discovery client for API version migration: %w", err)
	}

	resolved, err := resolveAPIVersionMigrations(discoveryClient, migrations)
	if err != nil {
		return nil, err
	}

	if len(resolved) == 0 {
		return nil, nil
	}

	return &apiVersionPostRenderer{migrations: resolved}, nil
}

func resolveAPIVersionMigrations(
	discoveryClient discovery.DiscoveryInterface,
	migrations []APIVersionMigration,
) (map[apiVersionMigrationKey]string, error) {
	resolved := make(map[apiVersionMigrationKey]string)

	for _, migration := range migrations {
		if migration.Kind == "" || migration.From == "" || migration.To == "" {
			return nil, errors.New("API version migration requires kind, source, and target")
		}

		fromServed, err := apiResourceServed(discoveryClient, migration.From, migration.Kind)
		if err != nil {
			return nil, fmt.Errorf("discover source API %s kind %s: %w", migration.From, migration.Kind, err)
		}

		if fromServed {
			continue
		}

		toServed, err := apiResourceServed(discoveryClient, migration.To, migration.Kind)
		if err != nil {
			return nil, fmt.Errorf("discover target API %s kind %s: %w", migration.To, migration.Kind, err)
		}

		if !toServed {
			return nil, fmt.Errorf(
				"no served API version for kind %s (checked %s and %s)",
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
		if apierrors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	for _, resource := range resources.APIResources {
		if resource.Kind == kind {
			return true, nil
		}
	}

	return false, nil
}

func (r *apiVersionPostRenderer) Run(renderedManifests *bytes.Buffer) (*bytes.Buffer, error) {
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
	if err := yaml.Unmarshal(document, &identity); err != nil {
		return nil, fmt.Errorf("read rendered manifest identity: %w", err)
	}

	target, ok := r.migrations[apiVersionMigrationKey{
		apiVersion: identity.APIVersion,
		kind:       identity.Kind,
	}]
	if !ok {
		return document, nil
	}

	apiVersionLine := regexp.MustCompile(
		`(?m)^(apiVersion:[\t ]*)` + regexp.QuoteMeta(identity.APIVersion) + `([\t ]*(?:#.*)?)$`,
	)
	if !apiVersionLine.Match(document) {
		return nil, fmt.Errorf(
			"rendered kind %s has no replaceable apiVersion %s",
			identity.Kind,
			identity.APIVersion,
		)
	}

	replacement := []byte("${1}" + target + "${2}")

	return apiVersionLine.ReplaceAll(document, replacement), nil
}
