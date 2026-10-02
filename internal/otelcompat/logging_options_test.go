package otelcompat_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"go.uber.org/zap"
)

// capturedLogProvider exports real SDK records through an in-memory HTTP transport.
func capturedLogProvider(
	t *testing.T,
) (*sdklog.LoggerProvider, chan *collector.ExportLogsServiceRequest) {
	t.Helper()

	requests := make(chan *collector.ExportLogsServiceRequest, 4)
	exporter, err := otlploghttp.New(t.Context(),
		otlploghttp.WithEndpointURL("http://logs.invalid/v1/logs"),
		otlploghttp.WithHTTPClient(&http.Client{Transport: requestCapture{requests: requests}}),
	)
	require.NoError(t, err)

	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	return provider, requests
}

// countedMessage exposes formatting side effects in a filtered logging call.
type countedMessage struct {
	calls *int
}

// String records whether the logger evaluated an otherwise suppressed value.
func (m countedMessage) String() string {
	*m.calls++

	return "failed"
}

// TestOTelZapFilteredFormattingRemainsLazy preserves filtering for irrelevant span status.
func TestOTelZapFilteredFormattingRemainsLazy(t *testing.T) {
	t.Parallel()

	provider := sdktrace.NewTracerProvider()

	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	ctx, span := provider.Tracer("ksail.logging").Start(t.Context(), "operation")
	defer span.End()

	require.True(t, trace.SpanFromContext(ctx).IsRecording())

	logger := otelzap.New(zap.NewNop(), otelzap.WithMinLevel(zap.DPanicLevel),
		otelzap.WithErrorStatusLevel(zap.WarnLevel), otelzap.WithCaller(false))
	calls := 0
	logger.Sugar().InfofContext(ctx, "operation %s", countedMessage{calls: &calls})
	logger.Sugar().WarnfContext(t.Context(), "operation %s", countedMessage{calls: &calls})
	require.Zero(
		t,
		calls,
		"suppressed messages with no qualifying recording span must stay unevaluated",
	)
}

// TestOTelZapCloneOptionsReachExporter catches stale provider and scope options in clones.
func TestOTelZapCloneOptionsReachExporter(t *testing.T) {
	t.Parallel()

	originalProvider, originalRequests := capturedLogProvider(t)
	cloneProvider, cloneRequests := capturedLogProvider(t)
	original := otelzap.New(zap.NewNop().Named("ksail.logging"),
		otelzap.WithLoggerProvider(originalProvider), otelzap.WithVersion("original"),
		otelzap.WithSchemaURL("https://schemas.invalid/original"), otelzap.WithCaller(false),
	)
	clone := original.Clone(otelzap.WithLoggerProvider(cloneProvider), otelzap.WithVersion("clone"),
		otelzap.WithSchemaURL("https://schemas.invalid/clone"))
	original.Ctx(t.Context()).Warn("original record")
	clone.Ctx(t.Context()).Warn("cloned record")

	flushCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, originalProvider.ForceFlush(flushCtx))
	require.NoError(t, cloneProvider.ForceFlush(flushCtx))

	for _, test := range []struct {
		name     string
		requests chan *collector.ExportLogsServiceRequest
		message  string
		schema   string
	}{
		{
			name: "original", requests: originalRequests, message: "original record",
			schema: "https://schemas.invalid/original",
		},
		{name: "clone", requests: cloneRequests, message: "cloned record", schema: "https://schemas.invalid/clone"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			select {
			case request := <-test.requests:
				require.Len(t, request.GetResourceLogs(), 1)
				scopes := request.GetResourceLogs()[0].GetScopeLogs()
				require.Len(t, scopes, 1)
				require.Equal(t, "ksail.logging", scopes[0].GetScope().GetName())
				require.Equal(t, test.name, scopes[0].GetScope().GetVersion())
				require.Equal(t, test.schema, scopes[0].GetSchemaUrl())
				records := scopes[0].GetLogRecords()
				require.Len(t, records, 1)
				require.Equal(t, test.message, records[0].GetBody().GetStringValue())
			default:
				t.Fatal("the configured provider exported no record after ForceFlush")
			}
		})
	}
}

type spanStatusLoggingPath struct {
	name string
	warn func(context.Context, *otelzap.Logger)
	info func(context.Context, *otelzap.Logger)
}

func spanStatusLoggingPaths() []spanStatusLoggingPath {
	return []spanStatusLoggingPath{
		{
			name: "structured",
			warn: func(ctx context.Context, l *otelzap.Logger) { l.WarnContext(ctx, "operation failed") },
			info: func(ctx context.Context, l *otelzap.Logger) { l.InfoContext(ctx, "operation succeeded") },
		},
		{
			name: "formatted", warn: func(ctx context.Context, l *otelzap.Logger) {
				l.Sugar().WarnfContext(ctx, "operation %s", "failed")
			},
			info: func(ctx context.Context, l *otelzap.Logger) {
				l.Sugar().InfofContext(ctx, "operation %s", "succeeded")
			},
		},
		{
			name: "key-values", warn: func(ctx context.Context, l *otelzap.Logger) {
				l.Sugar().WarnwContext(ctx, "operation failed", "attempt", 1)
			},
			info: func(ctx context.Context, l *otelzap.Logger) {
				l.Sugar().InfowContext(ctx, "operation succeeded", "attempt", 1)
			},
		},
	}
}

// TestOTelZapSpanStatusIsIndependentOfEmission catches span errors hidden by log filtering.
func TestOTelZapSpanStatusIsIndependentOfEmission(t *testing.T) {
	t.Parallel()

	for _, path := range spanStatusLoggingPaths() {
		for _, scenario := range []string{"suppressed-error", "suppressed-info", "emitted-error"} {
			t.Run(path.name+"/"+scenario, func(t *testing.T) {
				t.Parallel()

				exporter := tracetest.NewInMemoryExporter()
				tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

				t.Cleanup(
					func() { require.NoError(t, tracerProvider.Shutdown(context.Background())) },
				)
				ctx, span := tracerProvider.Tracer("ksail.logging").Start(t.Context(), "operation")
				capture := &captureLogger{}
				minLevel := zap.DPanicLevel

				wantEmitted := 0
				if scenario == "emitted-error" {
					minLevel, wantEmitted = zap.WarnLevel, 1
				}

				logger := otelzap.New(zap.NewNop(),
					otelzap.WithLoggerProvider(captureProvider{logger: capture}),
					otelzap.WithCaller(false), otelzap.WithMinLevel(minLevel),
					otelzap.WithErrorStatusLevel(zap.WarnLevel),
				)
				wantStatus := sdktrace.Status{Code: codes.Error, Description: "operation failed"}

				if scenario == "suppressed-info" {
					path.info(ctx, logger)

					wantStatus = sdktrace.Status{Code: codes.Unset}
				} else {
					path.warn(ctx, logger)
				}

				span.End()

				spans := exporter.GetSpans()
				require.Len(t, spans, 1)
				require.Equal(t, wantStatus, spans[0].Status)
				require.Equal(t, wantEmitted, capture.emitted)
			})
		}
	}
}
