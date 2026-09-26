package workload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/ephemeral"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/celrules"
	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const defaultObservationWait = 30 * time.Second

type ephemeralChildOptions struct {
	enabled bool
	wait    time.Duration
}

type childObserver interface {
	ObserveChildren(
		ctx context.Context,
		roots []*unstructured.Unstructured,
		delay time.Duration,
	) ([]*unstructured.Unstructured, error)
}

func addEphemeralChildFlags(cmd *cobra.Command, opts *ephemeralChildOptions) {
	cmd.Flags().BoolVar(&opts.enabled, "ephemeral-children", false,
		"EXPERIMENTAL: validate an observed snapshot of owner-linked descendants of directly submitted workloads "+
			"(requires --ephemeral; fails if none are observed; does not prove operator convergence)")
	cmd.Flags().DurationVar(&opts.wait, "ephemeral-observation-wait", defaultObservationWait,
		"Wait after admission before collecting children (positive, at most 5m; requires --ephemeral-children)")
}

func (o ephemeralChildOptions) validate(cmd *cobra.Command, enabled bool) error {
	if o.enabled && !enabled {
		return fmt.Errorf("%w: --ephemeral-children requires --ephemeral", ephemeral.ErrInput)
	}

	if !o.enabled && cmd.Flags().Changed("ephemeral-observation-wait") {
		return fmt.Errorf(
			"%w: --ephemeral-observation-wait requires --ephemeral-children",
			ephemeral.ErrInput,
		)
	}

	if o.enabled && (o.wait <= 0 || o.wait > ephemeral.MaxObservationWait) {
		return fmt.Errorf(
			"%w: --ephemeral-observation-wait must be positive and at most %s",
			ephemeral.ErrInput,
			ephemeral.MaxObservationWait,
		)
	}

	return nil
}

func runValidateWithChildren(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	flags validateFlags,
) error {
	validator, cleanup, err := prepareObservedValidator(ctx, cmd, args, flags)
	if err != nil {
		return err
	}
	defer cleanup()

	return withEphemeralChecks(ctx, cmd, args, ephemeralChecks{
		offline: func(ctx context.Context) error { return runValidateCmdInner(ctx, cmd, args, flags) },
		children: func(ctx context.Context, client ephemeral.Client, plan *ephemeral.Plan) error {
			observer, ok := client.(childObserver)
			if !ok {
				return fmt.Errorf(
					"%w: admission client cannot observe children",
					ephemeral.ErrObservation,
				)
			}

			notify.Infof(
				cmd.OutOrStdout(),
				"waiting %s before collecting operator-generated children...",
				flags.children.wait,
			)

			children, err := observer.ObserveChildren(ctx, plan.Resources, flags.children.wait)
			if err != nil {
				return fmt.Errorf("observe operator-generated children: %w", err)
			}

			if len(children) == 0 {
				return fmt.Errorf("%w: no children returned", ephemeral.ErrObservation)
			}

			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Writer:  cmd.ErrOrStderr(),
				Content: "observed %d operator-generated children after %s; this snapshot does not prove complete reconciliation",
				Args:    []any{len(children), flags.children.wait},
			})

			err = validator.validate(ctx, cmd, plan, children, flags.kyvernoPolicies)
			if err != nil {
				return fmt.Errorf("validate operator-generated children: %w", err)
			}

			notify.Infof(
				cmd.OutOrStdout(),
				"observed operator-generated children passed validation",
			)

			return nil
		},
	})
}

type observedValidator struct {
	opts   *kubeconform.ValidationOptions
	engine *celrules.Engine
}

// Prepare source-derived schema and rule context before provisioning or waiting.
func prepareObservedValidator(
	ctx context.Context, cmd *cobra.Command, args []string, flags validateFlags,
) (*observedValidator, func(), error) {
	cfg, found, loadErr := loadValidateConfigSilently(cmd)

	path, err := resolveEphemeralSourcePath(cmd, args)
	if err != nil {
		return nil, nil, err
	}

	opts, cleanup, err := buildValidationOptions(ctx, cmd, cfg, found, loadErr, flags, path,
		buildValidateRenderer(cfg, found, flags.skipHelmRender))
	if err != nil {
		return nil, nil, err
	}

	engine, err := buildCELEngine(resolveCELRulesPath(cmd, cfg, found, loadErr, flags.rules))
	if err != nil {
		cleanup()

		return nil, nil, err
	}

	return &observedValidator{opts: opts, engine: engine}, cleanup, nil
}

func (v *observedValidator) validate(
	ctx context.Context, cmd *cobra.Command, plan *ephemeral.Plan,
	children []*unstructured.Unstructured, kyverno bool,
) error {
	data, err := observedManifestBytes(children)
	if err != nil {
		return err
	}

	const source = "operator-generated children"

	err = kubeconform.NewClient().ValidateBytes(ctx, source, data, v.opts)
	if err != nil {
		return fmt.Errorf("validate observed manifests: %w", err)
	}

	sink := &celViolationSink{}
	defer sink.report(cmd)

	err = evaluateCELDocuments(v.engine, data, source, v.opts.SkipKinds, sink, nil)
	if err != nil {
		return err
	}

	if !kyverno {
		return nil
	}

	policySink := &celViolationSink{content: "Kyverno policy warning: %s"}
	defer policySink.report(cmd)

	return evaluateObservedKyverno(ctx, plan, data, v.opts.SkipKinds, policySink)
}

// Runtime state is excluded from the manifest view; payloads stay in memory.
func observedManifestBytes(children []*unstructured.Unstructured) ([]byte, error) {
	var data bytes.Buffer

	for _, child := range children {
		obj := child.DeepCopy()
		delete(obj.Object, "status")

		for _, field := range []string{"managedFields", "resourceVersion", "creationTimestamp", "generation", "selfLink"} {
			unstructured.RemoveNestedField(obj.Object, "metadata", field)
		}

		data.WriteString("---\n")

		err := json.NewEncoder(&data).Encode(obj.Object)
		if err != nil {
			return nil, fmt.Errorf("encode observed child: %w", err)
		}
	}

	return data.Bytes(), nil
}

func evaluateObservedKyverno(
	ctx context.Context,
	plan *ephemeral.Plan,
	data []byte,
	skipKinds []string,
	sink *celViolationSink,
) error {
	contextDocs := make([]map[string]any, 0, len(plan.Resources)+len(plan.Namespaces))
	for _, obj := range plan.Resources {
		contextDocs = append(contextDocs, obj.Object)
	}

	for _, obj := range plan.Namespaces {
		contextDocs = append(contextDocs, obj.Object)
	}

	policies, err := splitKyvernoPolicies(contextDocs, skipKinds)
	if err != nil {
		return err
	}

	targets, err := splitKyvernoPolicies(decodeDocuments(data), skipKinds)
	if err != nil {
		return err
	}

	engine := kyvernopolicy.NewEngine(policies.policies, contextDocs)

	celEngine, err := kyvernopolicy.NewCELEngine(policies.celPolicies, contextDocs)
	if err != nil {
		return fmt.Errorf("load observed-child Kyverno policies: %w", err)
	}

	blocking, err := evaluateKyvernoTargets(
		ctx,
		engine,
		celEngine,
		targets.targets,
		"operator-generated children",
		sink,
		nil,
	)
	if err != nil {
		return err
	}

	if len(blocking) > 0 {
		return fmt.Errorf("%w:\n  %s", ErrKyvernoPolicyViolation, strings.Join(blocking, "\n  "))
	}

	return nil
}
