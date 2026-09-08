package eks

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	awsconfigutil "github.com/devantler-tech/ksail/v7/pkg/awsconfig"
)

const (
	// v1SchemePrefix is the scheme aws-iam-authenticator defined for EKS
	// bearer tokens: the prefix followed by the base64url-encoded presigned
	// STS GetCallerIdentity URL.
	v1SchemePrefix = "k8s-aws-v1."
	// clusterIDHeader is the signed header that binds a presigned
	// GetCallerIdentity URL to one EKS cluster, so a token minted for this
	// cluster cannot authenticate against another.
	clusterIDHeader = "x-k8s-aws-id"
	// expiresHeader caps the presigned URL's validity; EKS rejects tokens
	// whose X-Amz-Expires exceeds 15 minutes.
	expiresHeader = "X-Amz-Expires"
	// presignExpirySeconds mirrors aws-iam-authenticator's 60-second default —
	// the operator consumes the token immediately, so the shortest standard
	// window is enough.
	presignExpirySeconds = "60"
)

var (
	errCallerIdentityUnavailable = errors.New(
		"getting AWS caller identity: identity client is unavailable",
	)
	errCallerAccountMissing = errors.New(
		"getting AWS caller identity: response did not include an account ID",
	)
)

// clusterDescriber is the narrow seam over the EKS SDK operation this
// package uses, so tests can inject a fake without AWS credentials.
// *awseks.Client satisfies it.
type clusterDescriber interface {
	DescribeCluster(
		ctx context.Context,
		params *awseks.DescribeClusterInput,
		optFns ...func(*awseks.Options),
	) (*awseks.DescribeClusterOutput, error)
}

// callerIdentityPresigner is the seam over the STS presign client that mints
// the signed URL inside an EKS bearer token. *sts.PresignClient satisfies it.
type callerIdentityPresigner interface {
	PresignGetCallerIdentity(
		ctx context.Context,
		params *sts.GetCallerIdentityInput,
		optFns ...func(*sts.PresignOptions),
	) (*v4.PresignedHTTPRequest, error)
}

// callerIdentityGetter is the seam over the read-only STS identity query used to bind lifecycle
// state to the currently selected AWS account. *sts.Client satisfies it.
type callerIdentityGetter interface {
	GetCallerIdentity(
		ctx context.Context,
		params *sts.GetCallerIdentityInput,
		optFns ...func(*sts.Options),
	) (*sts.GetCallerIdentityOutput, error)
}

// Client reads EKS cluster connection details and mints bearer tokens for
// them, hiding the SDK's request shapes and the token encoding scheme.
type Client struct {
	nodegroupStacks           *cloudformation.Client
	nodegroups                *awseks.Client
	describer                 clusterDescriber
	presigner                 callerIdentityPresigner
	identityGetter            callerIdentityGetter
	loadOptions               []func(*config.LoadOptions) error
	staticCredentialProvider  aws.CredentialsProvider
	credentialValuesAvailable bool
	requireCredentialValues   bool
	optionErr                 error
	awsConfig                 *aws.Config
}

// Option customises a Client.
type Option func(*Client)

// WithClusterDescriber injects the DescribeCluster implementation, letting
// tests substitute a fake for the real SDK client.
func WithClusterDescriber(describer clusterDescriber) Option {
	return func(client *Client) {
		client.describer = describer
	}
}

// WithCallerIdentityPresigner injects the STS presigner, letting tests
// substitute a fake or a statically-credentialed presign client.
func WithCallerIdentityPresigner(presigner callerIdentityPresigner) Option {
	return func(client *Client) {
		client.presigner = presigner
	}
}

// WithCallerIdentityGetter injects the read-only STS identity query used by ownership checks.
func WithCallerIdentityGetter(getter callerIdentityGetter) Option {
	return func(client *Client) {
		client.identityGetter = getter
	}
}

// WithCredentialValues pins the AWS identity used by the SDK-backed EKS and
// STS clients without mutating process environment. A complete static pair
// takes precedence over profile, matching the canonical AWS credential chain.
// Partial static credentials fail closed rather than falling back to an
// unrelated ambient identity.
func WithCredentialValues(profile, accessKeyID, secretAccessKey, sessionToken string) Option {
	return func(client *Client) {
		client.awsConfig = nil
		hasAccessKey := accessKeyID != ""
		hasSecretKey := secretAccessKey != ""

		if hasAccessKey != hasSecretKey || (sessionToken != "" && !hasAccessKey) {
			client.optionErr = ErrIncompleteStaticCredentials

			return
		}

		if hasAccessKey {
			client.credentialValuesAvailable = true
			client.staticCredentialProvider = awscredentials.NewStaticCredentialsProvider(
				accessKeyID,
				secretAccessKey,
				sessionToken,
			)

			return
		}

		if profile != "" {
			client.credentialValuesAvailable = true
			client.loadOptions = append(client.loadOptions, config.WithSharedConfigProfile(profile))
		}
	}
}

// WithAWSConfig pins a complete AWS SDK configuration snapshot. Callers can replace credentials
// while retaining resolved endpoint, HTTP, retry, and middleware settings without re-reading
// mutable process configuration.
func WithAWSConfig(config aws.Config) Option {
	return func(client *Client) {
		if config.Credentials == nil {
			client.optionErr = ErrExplicitCredentialsUnavailable

			return
		}

		config.ConfigSources = append(config.ConfigSources[:0:0], config.ConfigSources...)
		config.APIOptions = append(config.APIOptions[:0:0], config.APIOptions...)
		client.awsConfig = &config
		client.staticCredentialProvider = nil
		client.loadOptions = nil
		client.credentialValuesAvailable = true
	}
}

// RequireCredentialValues makes a custom credential selection fail closed
// when it resolves neither a profile nor a complete static key pair. This
// prevents the SDK from silently falling back to stale canonical environment
// credentials that the corresponding eksctl child environment removed.
func RequireCredentialValues() Option {
	return func(client *Client) {
		client.requireCredentialValues = true
	}
}

// NewClientWithCredentialRequirement constructs a Client from an immutable
// option snapshot, adding the fail-closed credential requirement when needed.
func NewClientWithCredentialRequirement(
	ctx context.Context,
	region string,
	required bool,
	options ...Option,
) (*Client, error) {
	result := append([]Option(nil), options...)
	if required {
		result = append(result, RequireCredentialValues())
	}

	return NewClient(ctx, region, result...)
}

// NewClient constructs a Client. Unless the existing DescribeCluster and token-presigning seams are
// both injected without explicit AWS configuration or credentials, it resolves the AWS
// configuration once and builds missing SDK clients. A
// deliberately partial injected client must also provide WithCallerIdentityGetter before using
// CallerAccountID; preserving that test seam avoids an unexpected config dependency for older
// consumers that only need DescribeCluster and MintToken.
func NewClient(ctx context.Context, region string, opts ...Option) (*Client, error) {
	client := &Client{
		describer:                 nil,
		presigner:                 nil,
		identityGetter:            nil,
		loadOptions:               nil,
		staticCredentialProvider:  nil,
		credentialValuesAvailable: false,
		requireCredentialValues:   false,
		optionErr:                 nil,
		awsConfig:                 nil,
	}

	for _, opt := range opts {
		opt(client)
	}

	if client.optionErr != nil {
		return nil, client.optionErr
	}

	if client.requireCredentialValues && !client.credentialValuesAvailable {
		return nil, ErrExplicitCredentialsUnavailable
	}

	err := client.configureMissingSDKClients(ctx, region)
	if err != nil {
		return nil, err
	}

	return client, nil
}

// CallerAccountID returns the 12-digit AWS account selected by this client's immutable credential
// configuration. The query never exposes or persists credentials.
func (c *Client) CallerAccountID(ctx context.Context) (string, error) {
	if c.identityGetter == nil {
		return "", errCallerIdentityUnavailable
	}

	out, err := c.identityGetter.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("getting AWS caller identity: %w", err)
	}

	if out == nil || strings.TrimSpace(aws.ToString(out.Account)) == "" {
		return "", errCallerAccountMissing
	}

	return strings.TrimSpace(aws.ToString(out.Account)), nil
}

// DescribeCluster returns the named EKS cluster's control-plane details.
func (c *Client) DescribeCluster(ctx context.Context, name string) (*ekstypes.Cluster, error) {
	out, err := c.describer.DescribeCluster(
		ctx,
		&awseks.DescribeClusterInput{Name: aws.String(name)},
	)
	if err != nil {
		// A cluster EKS has never heard of is absence, not a query failure, and it is the shape
		// absence actually takes in production — the empty-payload case below is the rarer one.
		// Reporting both through ErrClusterNotFound gives callers a single sentinel to test, so
		// "the cluster is gone" cannot be mistaken for "the lookup broke".
		var notFound *ekstypes.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return nil, fmt.Errorf("%w: %s: %w", ErrClusterNotFound, name, err)
		}

		return nil, fmt.Errorf("describing eks cluster %s: %w", name, err)
	}

	if out == nil || out.Cluster == nil {
		return nil, fmt.Errorf("%w: %s", ErrClusterNotFound, name)
	}

	return out.Cluster, nil
}

// MintToken mints the standard EKS bearer token for the named cluster: a
// presigned STS GetCallerIdentity URL carrying the cluster-binding header,
// base64url-encoded under the k8s-aws-v1 prefix. The token inherits the
// presigner's credentials and is valid for presignExpirySeconds.
func (c *Client) MintToken(ctx context.Context, clusterName string) (string, error) {
	request, err := c.presigner.PresignGetCallerIdentity(
		ctx,
		&sts.GetCallerIdentityInput{},
		func(options *sts.PresignOptions) {
			options.ClientOptions = append(options.ClientOptions, withTokenHeaders(clusterName))
		},
	)
	if err != nil {
		return "", fmt.Errorf("presigning sts get-caller-identity: %w", err)
	}

	return v1SchemePrefix + base64.RawURLEncoding.EncodeToString([]byte(request.URL)), nil
}

func (c *Client) configureMissingSDKClients(ctx context.Context, region string) error {
	if c.describer != nil && c.presigner != nil && !c.hasExplicitAWSConfiguration() {
		return nil
	}

	cfg, err := c.resolveAWSConfig(ctx, region)
	if err != nil {
		return err
	}

	err = c.configureMissingEKSClients(ctx, cfg)
	if err != nil {
		return err
	}

	return c.configureMissingSTSClients(ctx, cfg)
}

// resolveAWSConfig builds the AWS configuration the SDK clients are constructed from:
// an explicitly supplied config wins, then a static credential provider, and otherwise
// the default credential chain is loaded for the region.
func (c *Client) resolveAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	switch {
	case c.awsConfig != nil:
		cfg := *c.awsConfig
		cfg.ConfigSources = append(cfg.ConfigSources[:0:0], cfg.ConfigSources...)

		cfg.APIOptions = append(cfg.APIOptions[:0:0], cfg.APIOptions...)
		if strings.TrimSpace(region) != "" {
			cfg.Region = strings.TrimSpace(region)
		}

		return cfg, nil
	case c.staticCredentialProvider != nil:
		cfg, err := awsconfigutil.LoadNeutral(
			ctx,
			config.LoadDefaultConfig,
			region,
			c.staticCredentialProvider,
		)
		if err != nil {
			return aws.Config{}, fmt.Errorf("loading aws configuration: %w", err)
		}

		return cfg, nil
	default:
		loadOptions := append(
			[]func(*config.LoadOptions) error{config.WithRegion(region)},
			c.loadOptions...,
		)

		cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
		if err != nil {
			return aws.Config{}, fmt.Errorf("loading aws configuration: %w", err)
		}

		return cfg, nil
	}
}

func (c *Client) hasExplicitAWSConfiguration() bool {
	return c.awsConfig != nil || c.staticCredentialProvider != nil || len(c.loadOptions) > 0
}

func (c *Client) configureMissingEKSClients(ctx context.Context, cfg aws.Config) error {
	endpoint, frozen, err := awsconfigutil.FrozenServiceEndpoint(ctx, cfg, "EKS")
	if err != nil {
		return fmt.Errorf("resolve frozen EKS endpoint: %w", err)
	}

	if frozen {
		cfg.ServiceOptions = append([]func(string, any){func(_ string, options any) {
			if eksOptions, ok := options.(*awseks.Options); ok {
				eksOptions.BaseEndpoint = endpoint
			}
		}}, cfg.ServiceOptions...)
	}

	if c.nodegroups == nil {
		c.nodegroups = awseks.NewFromConfig(cfg)
	}

	if c.describer == nil {
		c.describer = c.nodegroups
	}
	if c.nodegroupStacks == nil {
		c.nodegroupStacks = cloudformation.NewFromConfig(cfg)
	}

	return nil
}

func (c *Client) configureMissingSTSClients(ctx context.Context, cfg aws.Config) error {
	if c.presigner != nil && c.identityGetter != nil {
		return nil
	}

	endpoint, frozen, err := awsconfigutil.FrozenServiceEndpoint(ctx, cfg, "STS")
	if err != nil {
		return fmt.Errorf("resolve frozen STS endpoint: %w", err)
	}

	if frozen {
		cfg.ServiceOptions = append([]func(string, any){func(_ string, options any) {
			if stsOptions, ok := options.(*sts.Options); ok {
				stsOptions.BaseEndpoint = endpoint
			}
		}}, cfg.ServiceOptions...)
	}

	stsClient := sts.NewFromConfig(cfg)
	if c.presigner == nil {
		c.presigner = sts.NewPresignClient(stsClient)
	}

	if c.identityGetter == nil {
		c.identityGetter = stsClient
	}

	return nil
}

// withTokenHeaders adds the signed headers that turn a plain presigned
// GetCallerIdentity URL into an EKS token: the cluster binding and the
// expiry cap.
func withTokenHeaders(clusterName string) func(*sts.Options) {
	return func(options *sts.Options) {
		options.APIOptions = append(
			options.APIOptions,
			smithyhttp.SetHeaderValue(clusterIDHeader, clusterName),
			smithyhttp.SetHeaderValue(expiresHeader, presignExpirySeconds),
		)
	}
}
