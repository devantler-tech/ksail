package kubeconform

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/yannh/kubeconform/pkg/validator"
)

// PrecacheSchemas fetches selected schemas before Flux placeholders are typed.
// Invalid resource values are not accepted here: the caller must validate the
// final expanded data. Schema/parse errors fail, including unavailable schemas
// even when ordinary validation permits missing schemas. Kind exclusions remain.
func (c *Client) PrecacheSchemas(ctx context.Context, data []byte, opts *ValidationOptions) error {
	if opts != nil && opts.schemaRefresh != nil {
		opts.schemaRefresh.prepareMu.Lock()
		defer opts.schemaRefresh.prepareMu.Unlock()
	}

	err := ctx.Err()
	if err != nil {
		return fmt.Errorf("precache schemas: %w", err)
	}

	strictOpts := ValidationOptions{}
	if opts != nil {
		strictOpts = *opts
	}

	strictOpts.IgnoreMissingSchemas = false

	documents, err := splitDocumentsForValidation(data)
	if err != nil {
		return fmt.Errorf("split manifests for schema refresh: %w", err)
	}

	engine, err := c.createValidator(&strictOpts)
	if err != nil {
		return fmt.Errorf("create schema refresh validator: %w", err)
	}

	return c.validateWithRetry(ctx, func() error {
		return c.precacheDocuments(ctx, engine, documents, strictOpts.Attribution)
	})
}

// precacheDocuments reports loader errors while leaving value validation to the final pass.
func (c *Client) precacheDocuments(
	ctx context.Context,
	engine validator.Validator,
	documents [][]byte,
	attribution map[string]string,
) error {
	var failures []validator.Result

	for _, document := range documents {
		err := ctx.Err()
		if err != nil {
			return fmt.Errorf("precache schemas: %w", err)
		}

		results := engine.ValidateWithContext(
			ctx,
			"schema-refresh",
			io.NopCloser(bytes.NewReader(document)),
		)
		for _, result := range results {
			if result.Status == validator.Error {
				failures = append(failures, result)
			}
		}
	}

	return c.processResults(failures, attribution)
}
