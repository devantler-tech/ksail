package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
)

var (
	// ErrIncompleteAWSStaticCredentials reports a partial static credential tuple that must never
	// fall through to another AWS identity provider.
	ErrIncompleteAWSStaticCredentials = errors.New("incomplete AWS static credentials")
	// ErrExplicitAWSCredentialsUnavailable reports configured custom sources that resolved no usable
	// profile or static tuple.
	ErrExplicitAWSCredentialsUnavailable = errors.New("explicit AWS credentials are unavailable")
	// ErrAWSCredentialsUnavailable reports an AWS configuration whose provider returned no usable
	// concrete access and secret key pair.
	ErrAWSCredentialsUnavailable = errors.New("AWS credentials are unavailable")
)

type awsConfigLoader func(
	context.Context,
	...func(*config.LoadOptions) error,
) (aws.Config, error)

const frozenAWSProfile = "__ksail_frozen__"

// ResolveFrozenAWS resolves the selected AWS provider exactly once and returns a concrete static
// credential tuple. The tuple can be reused by SDK clients and eksctl child processes without
// re-reading a rotating profile, credential_process, SSO, web-identity, or default provider chain.
func ResolveFrozenAWS(
	ctx context.Context,
	resolver Resolver,
	region string,
) (AWSResolution, error) {
	return FreezeAWS(ctx, ResolveAWS(resolver), region)
}

// FreezeAWS resolves an existing immutable selection to one concrete credential tuple.
func FreezeAWS(
	ctx context.Context,
	selection AWSResolution,
	region string,
) (AWSResolution, error) {
	return freezeAWSResolution(ctx, region, selection, config.LoadDefaultConfig)
}

func freezeAWSResolution(
	ctx context.Context,
	region string,
	selection AWSResolution,
	loader awsConfigLoader,
) (AWSResolution, error) {
	loadOptions, err := frozenAWSLoadOptions(region, selection)
	if err != nil {
		return AWSResolution{}, err
	}

	if strings.TrimSpace(region) != "" {
		selection.Region = strings.TrimSpace(region)
	}

	cfg, err := loadFrozenAWSConfig(ctx, loader, region, selection, loadOptions)
	if err != nil {
		return AWSResolution{}, fmt.Errorf(
			"load AWS configuration for credential snapshot: %w",
			err,
		)
	}

	credentialValues, err := resolveConcreteAWSCredentials(ctx, cfg, selection)
	if err != nil {
		return AWSResolution{}, err
	}

	if strings.TrimSpace(selection.Region) == "" {
		selection.Region = frozenAWSRegion(selection, cfg)
	}

	selection.Profile = ""
	selection.AccessKeyID = credentialValues.AccessKeyID
	selection.SecretAccessKey = credentialValues.SecretAccessKey
	selection.SessionToken = credentialValues.SessionToken
	selection.frozen = true

	frozenProvider := awscredentials.NewStaticCredentialsProvider(
		credentialValues.AccessKeyID,
		credentialValues.SecretAccessKey,
		credentialValues.SessionToken,
	)
	cfg = sanitizeAWSConfigIdentity(cfg, selection.Region)
	cfg.Region = selection.Region
	cfg.Credentials = aws.NewCredentialsCache(frozenProvider)
	selection.sdkConfig = &cfg

	return selection, nil
}

func loadFrozenAWSConfig(
	ctx context.Context,
	loader awsConfigLoader,
	region string,
	selection AWSResolution,
	loadOptions []func(*config.LoadOptions) error,
) (aws.Config, error) {
	hasStaticCredentials := selection.AccessKeyID != ""
	if hasStaticCredentials && selection.usesUnsetCustomProfileSource() {
		return loadNeutralAWSConfig(ctx, loader, region, selection)
	}

	cfg, err := loader(ctx, loadOptions...)
	if err == nil || !hasStaticCredentials || !isMissingSharedConfigProfile(err) {
		return cfg, err
	}

	// A complete static tuple is authoritative. A stale ambient profile must not block it,
	// but valid profile/default non-identity settings are retained when the first load works.
	return loadNeutralAWSConfig(ctx, loader, region, selection)
}

func resolveConcreteAWSCredentials(
	ctx context.Context,
	cfg aws.Config,
	selection AWSResolution,
) (aws.Credentials, error) {
	if selection.AccessKeyID != "" {
		return aws.Credentials{
			AccessKeyID:     selection.AccessKeyID,
			SecretAccessKey: selection.SecretAccessKey,
			SessionToken:    selection.SessionToken,
		}, nil
	}

	if cfg.Credentials == nil {
		return aws.Credentials{}, ErrAWSCredentialsUnavailable
	}

	credentialValues, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, fmt.Errorf("retrieve concrete AWS credentials: %w", err)
	}

	if credentialValues.AccessKeyID == "" || credentialValues.SecretAccessKey == "" {
		return aws.Credentials{}, ErrAWSCredentialsUnavailable
	}

	return credentialValues, nil
}

func frozenAWSLoadOptions(
	region string,
	selection AWSResolution,
) ([]func(*config.LoadOptions) error, error) {
	hasAccessKey := selection.AccessKeyID != ""
	hasSecretKey := selection.SecretAccessKey != ""

	if hasAccessKey != hasSecretKey || (selection.SessionToken != "" && !hasAccessKey) {
		return nil, ErrIncompleteAWSStaticCredentials
	}

	if selection.HasCustomCredentialSources() && selection.Profile == "" && !hasAccessKey {
		return nil, ErrExplicitAWSCredentialsUnavailable
	}

	options := []func(*config.LoadOptions) error{config.WithRegion(region)}
	if hasAccessKey {
		options = append(options, config.WithCredentialsProvider(
			awscredentials.NewStaticCredentialsProvider(
				selection.AccessKeyID,
				selection.SecretAccessKey,
				selection.SessionToken,
			),
		))
	}

	if selection.Profile != "" {
		options = append(options, config.WithSharedConfigProfile(selection.Profile))
	}

	return options, nil
}

func loadNeutralAWSConfig(
	ctx context.Context,
	loader awsConfigLoader,
	region string,
	selection AWSResolution,
) (aws.Config, error) {
	file, err := os.CreateTemp("", "ksail-frozen-aws-config-*")
	if err != nil {
		return aws.Config{}, fmt.Errorf("create neutral AWS config: %w", err)
	}

	path := file.Name()
	defer func() { _ = os.Remove(path) }()

	_, writeErr := fmt.Fprintf(file, "[profile %s]\n", frozenAWSProfile)
	closeErr := file.Close()

	if writeErr != nil {
		return aws.Config{}, fmt.Errorf("write neutral AWS config: %w", writeErr)
	}

	if closeErr != nil {
		return aws.Config{}, fmt.Errorf("close neutral AWS config: %w", closeErr)
	}

	provider := awscredentials.NewStaticCredentialsProvider(
		selection.AccessKeyID,
		selection.SecretAccessKey,
		selection.SessionToken,
	)
	options := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithCredentialsProvider(provider),
		config.WithSharedConfigProfile(frozenAWSProfile),
		config.WithSharedConfigFiles([]string{path}),
		config.WithSharedCredentialsFiles([]string{}),
	}

	return loader(ctx, options...)
}

func isMissingSharedConfigProfile(err error) bool {
	var profileErr config.SharedConfigProfileNotExistError

	return errors.As(err, &profileErr)
}

func (r AWSResolution) usesUnsetCustomProfileSource() bool {
	return r.Profile == "" && r.sourceEnvVars[0] != "" &&
		r.sourceEnvVars[0] != defaultAWSProfileEnvVar
}

func frozenAWSRegion(selection AWSResolution, cfg aws.Config) string {
	if !selection.usesUnsetCustomRegionSource() {
		return strings.TrimSpace(cfg.Region)
	}

	// A custom region alias that resolved empty must not fall back to stale canonical AWS_REGION.
	// A valid selected/default shared profile may still provide its non-credential region.
	for _, source := range cfg.ConfigSources {
		switch value := source.(type) {
		case config.SharedConfig:
			if region := strings.TrimSpace(value.Region); region != "" {
				return region
			}
		case *config.SharedConfig:
			if value != nil {
				if region := strings.TrimSpace(value.Region); region != "" {
					return region
				}
			}
		}
	}

	return ""
}

func (r AWSResolution) usesUnsetCustomRegionSource() bool {
	return r.Region == "" && r.sourceRegionEnvVar != "" &&
		r.sourceRegionEnvVar != defaultAWSRegionEnvVar
}

func sanitizeAWSConfigIdentity(cfg aws.Config, region string) aws.Config {
	cfg.BearerAuthTokenProvider = nil
	cfg.ConfigSources = sanitizeAWSConfigSources(cfg.ConfigSources, region)
	cfg.APIOptions = append(cfg.APIOptions[:0:0], cfg.APIOptions...)

	return cfg
}

func sanitizeAWSConfigSources(sources []any, region string) []any {
	sanitized := make([]any, 0, len(sources))
	for _, source := range sources {
		switch value := source.(type) {
		case config.EnvConfig:
			sanitized = append(sanitized, sanitizeAWSEnvConfig(value, region))
		case config.SharedConfig:
			sanitized = append(sanitized, sanitizeAWSSharedConfig(value, region))
		case config.LoadOptions:
			sanitized = append(sanitized, sanitizeAWSLoadOptions(value, region))
		default:
			sanitized = append(sanitized, source)
		}
	}

	return sanitized
}

func sanitizeAWSEnvConfig(value config.EnvConfig, region string) config.EnvConfig {
	value.Credentials = aws.Credentials{}
	value.ContainerCredentialsEndpoint = ""
	value.ContainerCredentialsRelativePath = ""
	value.ContainerAuthorizationToken = ""
	value.SharedConfigProfile = ""
	value.SharedCredentialsFile = ""
	value.WebIdentityTokenFilePath = ""
	value.RoleARN = ""
	value.RoleSessionName = ""
	value.Region = region

	return value
}

func sanitizeAWSSharedConfig(value config.SharedConfig, region string) config.SharedConfig {
	value.Profile = ""
	value.Credentials = aws.Credentials{}
	value.CredentialSource = ""
	value.CredentialProcess = ""
	value.WebIdentityTokenFile = ""
	value.SSOSessionName = ""
	value.SSOSession = nil
	value.SSORegion = ""
	value.SSOStartURL = ""
	value.SSOAccountID = ""
	value.SSORoleName = ""
	value.RoleARN = ""
	value.ExternalID = ""
	value.MFASerial = ""
	value.RoleSessionName = ""
	value.RoleDurationSeconds = nil
	value.SourceProfileName = ""
	value.Source = nil
	value.LoginSession = ""
	value.Region = region

	return value
}

func sanitizeAWSLoadOptions(value config.LoadOptions, region string) config.LoadOptions {
	value.Region = region
	value.Credentials = nil
	value.BearerAuthTokenProvider = nil
	value.SharedConfigProfile = ""
	value.SharedConfigFiles = nil
	value.SharedCredentialsFiles = nil
	value.CredentialsCacheOptions = nil
	value.BearerAuthTokenCacheOptions = nil
	value.SSOTokenProviderOptions = nil
	value.ProcessCredentialOptions = nil
	value.EC2RoleCredentialOptions = nil
	value.EndpointCredentialOptions = nil
	value.WebIdentityRoleCredentialOptions = nil
	value.AssumeRoleCredentialOptions = nil
	value.SSOProviderOptions = nil

	return value
}
