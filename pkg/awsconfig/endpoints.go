package awsconfig

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

type endpointEnvironment map[string]string

// FrozenEnvironmentConfig retains SDK environment settings while resolving service
// endpoints from the captured environment instead of the current process.
type FrozenEnvironmentConfig struct {
	config.EnvConfig

	endpoints endpointEnvironment
}

// GetServiceBaseEndpoint implements the SDK source interface using the captured values.
func (e FrozenEnvironmentConfig) GetServiceBaseEndpoint(
	_ context.Context, service string,
) (string, bool, error) {
	value := e.endpoints[serviceEndpointKey(service)]

	return value, value != "", nil
}

func serviceEndpointKey(service string) string {
	return "AWS_ENDPOINT_URL_" + strings.ReplaceAll(strings.ToUpper(service), " ", "_")
}

func capturedEndpointEnvironment(sources []any) endpointEnvironment {
	for _, source := range sources {
		if snapshot, ok := source.(endpointEnvironment); ok {
			return snapshot
		}
	}

	return nil
}

func captureEndpointEnvironment() endpointEnvironment {
	endpoints := endpointEnvironment{}

	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found && (key == "AWS_ENDPOINT_URL" || strings.HasPrefix(key, "AWS_ENDPOINT_URL_")) {
			endpoints[key] = value
		}
	}

	return endpoints
}

// FreezeEndpointSources snapshots endpoint values and presence while retaining all
// other SDK source interfaces. Repeated freezing preserves the first snapshot.
func FreezeEndpointSources(cfg aws.Config) aws.Config {
	if capturedEndpointEnvironment(cfg.ConfigSources) != nil {
		return cfg
	}

	endpoints := captureEndpointEnvironment()

	sources := make([]any, 0, len(cfg.ConfigSources)+1)
	for _, source := range cfg.ConfigSources {
		sources = append(sources, freezeEndpointSource(source, endpoints))
	}

	sources = append(sources, endpoints)
	cfg.ConfigSources = sources

	return cfg
}

func freezeEndpointSource(source any, endpoints endpointEnvironment) any {
	switch value := source.(type) {
	case config.EnvConfig:
		return FrozenEnvironmentConfig{EnvConfig: value, endpoints: endpoints}
	case *config.EnvConfig:
		if value != nil {
			return FrozenEnvironmentConfig{EnvConfig: *value, endpoints: endpoints}
		}
	}

	return source
}

type serviceEndpointProvider interface {
	GetServiceBaseEndpoint(ctx context.Context, service string) (string, bool, error)
}

type ignoreEndpointsProvider interface {
	GetIgnoreConfiguredEndpoints(ctx context.Context) (bool, bool, error)
}

// FrozenServiceEndpoint resolves a service endpoint from a frozen snapshot. Apply the
// result before explicit service options: generated constructors also inspect current
// environment presence outside ConfigSources. A nil endpoint preserves AWS defaults;
// frozen=false leaves an ordinary SDK configuration alone.
func FrozenServiceEndpoint(
	ctx context.Context, cfg aws.Config, service string,
) (*string, bool, error) {
	environment := capturedEndpointEnvironment(cfg.ConfigSources)
	if environment == nil {
		return nil, false, nil
	}

	_, globalPresent := environment["AWS_ENDPOINT_URL"]

	_, servicePresent := environment[serviceEndpointKey(service)]
	if globalPresent && !servicePresent {
		return cfg.BaseEndpoint, true, nil
	}

	ignored, err := configuredEndpointsIgnored(ctx, cfg.ConfigSources)
	if err != nil {
		return nil, true, err
	}

	if ignored {
		return cfg.BaseEndpoint, true, nil
	}

	endpoint, err := resolveFrozenServiceEndpoint(ctx, cfg, service)

	return endpoint, true, err
}

func resolveFrozenServiceEndpoint(
	ctx context.Context,
	cfg aws.Config,
	service string,
) (*string, error) {
	for _, source := range cfg.ConfigSources {
		provider, ok := source.(serviceEndpointProvider)
		if !ok {
			continue
		}

		value, found, err := provider.GetServiceBaseEndpoint(ctx, service)
		if err != nil {
			return nil, fmt.Errorf("resolve frozen %s endpoint: %w", service, err)
		}

		if found {
			return aws.String(value), nil
		}
	}

	return cfg.BaseEndpoint, nil
}

func configuredEndpointsIgnored(ctx context.Context, sources []any) (bool, error) {
	for _, source := range sources {
		provider, ok := source.(ignoreEndpointsProvider)
		if !ok {
			continue
		}

		ignored, found, err := provider.GetIgnoreConfiguredEndpoints(ctx)
		if err != nil {
			return false, fmt.Errorf("resolve frozen endpoint policy: %w", err)
		}

		if found {
			return ignored, nil
		}
	}

	return false, nil
}
