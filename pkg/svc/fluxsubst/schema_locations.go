package fluxsubst

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/yannh/kubeconform/pkg/cache"
	"github.com/yannh/kubeconform/pkg/registry"
)

// schemaFallbackLocationCount includes Kubernetes built-ins and the CRD catalogue.
const schemaFallbackLocationCount = 2

const defaultSchemaLocation = "https://raw.githubusercontent.com/" +
	"yannh/kubernetes-json-schema/master/" +
	"{{.NormalizedKubernetesVersion}}-standalone{{.StrictSuffix}}/" +
	"{{.ResourceKind}}{{.KindSuffix}}.json"

const catalogSchemaLocation = "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/" +
	"{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"

// SchemaOptions selects the same schema locations and strictness as validation.
// Remote locations are read only from kubeconform's existing disk cache; local
// locations are read through kubeconform's native filesystem registry.
type SchemaOptions struct {
	SchemaLocations []string
	Strict          bool
	CacheDirectory  string
	// DeferredKinds is keyed by apiVersion/kind. During CRD discovery, a
	// matching document without a schema retains its original placeholders.
	// Callers must subsequently expand it and validate against the derived schemas.
	DeferredKinds map[string]struct{}
}

// fetchSchemaWithOptions preserves validator precedence: built-in Kubernetes,
// caller-supplied locations, then the CRD catalogue. A missing or malformed schema
// keeps the existing string fallback; actual validation still reports its errors.
func fetchSchemaWithOptions(apiVersion, kind string, opts SchemaOptions) map[string]any {
	locations := make([]string, 0, len(opts.SchemaLocations)+schemaFallbackLocationCount)
	locations = append(locations, "default")
	locations = append(locations, opts.SchemaLocations...)
	locations = append(locations, catalogSchemaLocation)

	cacheDir := opts.CacheDirectory
	if cacheDir == "" {
		cacheDir = schemaCacheDir()
	}

	disk := cache.NewOnDiskCache(cacheDir)

	for _, location := range locations {
		if schema := fetchLocationSchema(location, apiVersion, kind, opts, disk); schema != nil {
			return schema
		}
	}

	return nil
}

// fetchLocationSchema reads a local schema natively or a remote schema solely from disk.
func fetchLocationSchema(
	location, apiVersion, kind string,
	opts SchemaOptions,
	disk cache.Cache,
) map[string]any {
	if location == "default" || strings.HasPrefix(location, "http") {
		url, err := resolveSchemaLocation(location, apiVersion, kind, opts.Strict)
		if err != nil {
			return nil
		}

		return readCachedSchema(disk, url)
	}

	local, err := registry.New(location, "", opts.Strict, false, false)
	if err != nil {
		return nil
	}

	_, value, err := local.DownloadSchema(kind, apiVersion, "master")
	if err != nil {
		return nil
	}

	schema, _ := value.(map[string]any)

	return schema
}

// readCachedSchema decodes a downloaded schema, retaining fallback on unreadable or invalid data.
func readCachedSchema(disk cache.Cache, url string) map[string]any {
	value, err := disk.Get(url)
	if err != nil {
		return nil
	}

	data, ok := value.([]byte)
	if !ok {
		return nil
	}

	return parseJSONSchema(data)
}

// resolveSchemaLocation mirrors kubeconform v0.8.0's unexported schemaPath
// template rules so a remote lookup can use its exact cache key without invoking
// its network loader. Command tests warm the native validator and then close the
// fixture server before exercising these template and directory lookups.
func resolveSchemaLocation(
	location, apiVersion, kind string,
	strict bool,
) (string, error) {
	if location == "default" {
		location = defaultSchemaLocation
	} else if !strings.HasSuffix(location, "json") {
		location += "/{{.NormalizedKubernetesVersion}}-standalone{{.StrictSuffix}}/" +
			"{{.ResourceKind}}{{.KindSuffix}}.json"
	}

	parts := strings.Split(apiVersion, "/")
	groupLabels := strings.Split(parts[0], ".")

	kindSuffix := "-" + strings.ToLower(groupLabels[0])
	if len(parts) > 1 {
		kindSuffix += "-" + strings.ToLower(parts[1])
	}

	strictSuffix := ""
	if strict {
		strictSuffix = "-strict"
	}

	tpl, err := template.New("schema").Parse(location)
	if err != nil {
		return "", fmt.Errorf("parse schema location: %w", err)
	}

	var result bytes.Buffer

	err = tpl.Execute(&result, struct {
		NormalizedKubernetesVersion string
		StrictSuffix                string
		ResourceKind                string
		ResourceAPIVersion          string
		Group                       string
		KindSuffix                  string
	}{"master", strictSuffix, strings.ToLower(kind), parts[len(parts)-1], parts[0], kindSuffix})
	if err != nil {
		return "", fmt.Errorf("execute schema location: %w", err)
	}

	return result.String(), nil
}
