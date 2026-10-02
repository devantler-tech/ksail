// Package otelcompat_test exercises the adapter selected by the root module.
package otelcompat_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/noop"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// captureProvider preserves the context and record delivered through the public API.
type captureProvider struct {
	noop.LoggerProvider
	logger *captureLogger
}

func (p captureProvider) Logger(_ string, _ ...otellog.LoggerOption) otellog.Logger {
	return p.logger
}

type captureLogger struct {
	noop.Logger
	ctx    context.Context
	record otellog.Record
}

func (l *captureLogger) Emit(ctx context.Context, record otellog.Record) {
	l.ctx = ctx
	l.record = record.Clone()
}

func TestOTelZapStructuredFields(t *testing.T) {
	t.Parallel()

	capture := &captureLogger{}
	logger := otelzap.New(zap.NewNop(),
		otelzap.WithLoggerProvider(captureProvider{logger: capture}),
		otelzap.WithCaller(false),
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	logger.Ctx(ctx).Warn("structured record",
		zap.Bool("enabled", true), zap.Int64("attempt", 42), zap.Float64("ratio", 1.25),
		zap.String("name", "ksail"), zap.Binary("payload", []byte{0, 1, 255}),
		zap.Duration("delay", 3*time.Second), zap.Error(io.EOF),
	)

	require.Same(t, ctx, capture.ctx)
	require.Equal(t, "structured record", capture.record.Body().AsString())
	require.Equal(t, otellog.SeverityWarn, capture.record.Severity())
	attrs := make(map[string]any)
	capture.record.WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
		return true
	})
	require.Equal(t, map[string]any{
		"enabled": true, "attempt": int64(42), "ratio": 1.25, "name": "ksail",
		"payload": []byte{0, 1, 255}, "delay": int64(3 * time.Second),
		"exception.type": "*errors.errorString", "exception.message": "EOF",
	}, attrs)
}

func TestOTelZapSugaredValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "nil", value: nil, want: "<nil>"},
		{name: "string", value: "ksail", want: "ksail"},
		{name: "integer", value: 42, want: int64(42)},
		{name: "unsigned", value: uint64(43), want: int64(43)},
		{name: "boolean", value: true, want: true},
		{name: "float", value: 1.25, want: 1.25},
		{name: "stringer", value: time.Second, want: "1s"},
		{name: "slice", value: []any{"ksail", 42, true}, want: []attribute.Value{
			attribute.StringValue("ksail"), attribute.Int64Value(42), attribute.BoolValue(true),
		}},
		{name: "array", value: [2]int{1, 2}, want: []attribute.Value{
			attribute.Int64Value(1), attribute.Int64Value(2),
		}},
		{name: "nested", value: [][]int{{1}, {2}}, want: []attribute.Value{
			attribute.SliceValue(attribute.Int64Value(1)), attribute.SliceValue(attribute.Int64Value(2)),
		}},
		{name: "json", value: map[string]int{"attempt": 42}, want: `{"attempt":42}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			capture := &captureLogger{}
			logger := otelzap.New(zap.NewNop(),
				otelzap.WithLoggerProvider(captureProvider{logger: capture}),
				otelzap.WithCaller(false),
			)
			logger.Sugar().Ctx(t.Context()).Warnw("sugared record", "value", test.value)
			require.Equal(t, "sugared record", capture.record.Body().AsString())
			require.Equal(t, 1, capture.record.AttributesLen())
			capture.record.WalkAttributes(func(kv attribute.KeyValue) bool {
				require.Equal(t, "value", string(kv.Key))
				require.Equal(t, test.want, kv.Value.AsInterface())
				return true
			})
		})
	}
}

type requestCapture struct {
	requests chan *collector.ExportLogsServiceRequest
}

// RoundTrip captures the real exporter's protobuf request without opening a connection.
func (c requestCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read log request: %w", err)
	}
	var payload collector.ExportLogsServiceRequest
	if err = proto.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode log request: %w", err)
	}
	select {
	case c.requests <- &payload:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")),
		Header: make(http.Header), Request: req,
	}, nil
}

func TestOTelZapFixedBatchProcessorExportsTraceContext(t *testing.T) {
	t.Parallel()

	requests := make(chan *collector.ExportLogsServiceRequest, 1)
	exporter, err := otlploghttp.New(t.Context(),
		otlploghttp.WithEndpointURL("http://logs.invalid/v1/logs"),
		otlploghttp.WithHTTPClient(&http.Client{Transport: requestCapture{requests: requests}}),
	)
	require.NoError(t, err)
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	logger := otelzap.New(zap.NewNop(), otelzap.WithLoggerProvider(provider), otelzap.WithCaller(false))
	span := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1, 2, 3}, SpanID: trace.SpanID{4, 5, 6}, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(t.Context(), span)
	logger.Ctx(ctx).Warn("exported record", zap.Binary("payload", []byte{0, 1, 255}), zap.Int("attempt", 42))
	flushCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, provider.ForceFlush(flushCtx))

	select {
	case request := <-requests:
		require.Len(t, request.GetResourceLogs(), 1)
		require.Len(t, request.GetResourceLogs()[0].GetScopeLogs(), 1)
		records := request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()
		require.Len(t, records, 1)
		record := records[0]
		require.Equal(t, "exported record", record.GetBody().GetStringValue())
		require.EqualValues(t, otellog.SeverityWarn, record.GetSeverityNumber())
		traceID, spanID := span.TraceID(), span.SpanID()
		require.True(t, bytes.Equal(traceID[:], record.GetTraceId()))
		require.True(t, bytes.Equal(spanID[:], record.GetSpanId()))
		attrs := make(map[string]*common.AnyValue)
		for _, kv := range record.GetAttributes() {
			attrs[kv.GetKey()] = kv.GetValue()
		}
		require.Equal(t, []byte{0, 1, 255}, attrs["payload"].GetBytesValue())
		require.Equal(t, int64(42), attrs["attempt"].GetIntValue())
	case <-flushCtx.Done():
		t.Fatal("the fixed batch processor exported no log record")
	}
}
