package eks

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithyhttp "github.com/aws/smithy-go/transport/http"
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

// Client reads EKS cluster connection details and mints bearer tokens for
// them, hiding the SDK's request shapes and the token encoding scheme.
type Client struct {
	describer                 clusterDescriber
	presigner                 callerIdentityPresigner
	loadOptions               []func(*config.LoadOptions) error
	credentialValuesAvailable bool
	requireCredentialValues   bool
	optionErr                 error
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

// WithCredentialValues pins the AWS identity used by the SDK-backed EKS and
// STS clients without mutating process environment. A complete static pair
// takes precedence over profile, matching the canonical AWS credential chain.
// Partial static credentials fail closed rather than falling back to an
// unrelated ambient identity.
func WithCredentialValues(profile, accessKeyID, secretAccessKey, sessionToken string) Option {
	return func(client *Client) {
		hasAccessKey := accessKeyID != ""
		hasSecretKey := secretAccessKey != ""

		if hasAccessKey != hasSecretKey || (sessionToken != "" && !hasAccessKey) {
			client.optionErr = ErrIncompleteStaticCredentials

			return
		}

		if hasAccessKey {
			client.credentialValuesAvailable = true
			client.loadOptions = append(
				client.loadOptions,
				config.WithCredentialsProvider(awscredentials.NewStaticCredentialsProvider(
					accessKeyID,
					secretAccessKey,
					sessionToken,
				)),
			)

			return
		}

		if profile != "" {
			client.credentialValuesAvailable = true
			client.loadOptions = append(client.loadOptions, config.WithSharedConfigProfile(profile))
		}
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

// NewClient constructs a Client. Unless both seams are injected, it resolves
// the AWS default configuration (env, shared config, IRSA / instance
// role) once and builds the real SDK clients from it.
func NewClient(ctx context.Context, region string, opts ...Option) (*Client, error) {
	client := &Client{
		describer:                 nil,
		presigner:                 nil,
		loadOptions:               nil,
		credentialValuesAvailable: false,
		requireCredentialValues:   false,
		optionErr:                 nil,
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

	if client.describer == nil || client.presigner == nil {
		loadOptions := append(
			[]func(*config.LoadOptions) error{config.WithRegion(region)},
			client.loadOptions...,
		)

		cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
		if err != nil {
			return nil, fmt.Errorf("loading aws configuration: %w", err)
		}

		if client.describer == nil {
			client.describer = awseks.NewFromConfig(cfg)
		}

		if client.presigner == nil {
			client.presigner = sts.NewPresignClient(sts.NewFromConfig(cfg))
		}
	}

	return client, nil
}

// DescribeCluster returns the named EKS cluster's control-plane details.
func (c *Client) DescribeCluster(ctx context.Context, name string) (*ekstypes.Cluster, error) {
	out, err := c.describer.DescribeCluster(
		ctx,
		&awseks.DescribeClusterInput{Name: aws.String(name)},
	)
	if err != nil {
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
