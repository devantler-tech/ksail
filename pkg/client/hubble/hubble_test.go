package hubble_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	observerpb "github.com/cilium/cilium/api/v1/observer"
	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	errObserve = errors.New("boom")
	errEmit    = errors.New("emit failed")
)

func sampleRecords() []hubble.FlowRecord {
	first := time.Date(2026, time.June, 29, 8, 0, 0, 0, time.UTC)
	second := first.Add(time.Second)

	return []hubble.FlowRecord{
		{
			Time:        &first,
			Verdict:     "FORWARDED",
			Protocol:    "TCP",
			Source:      hubble.Endpoint{Namespace: "default", Pod: "web-abc123"},
			Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "coredns-xyz"},
		},
		{
			Time:        &second,
			Verdict:     "DROPPED",
			Protocol:    "UDP",
			Source:      hubble.Endpoint{Namespace: "monitoring", Pod: "prom-1"},
			Destination: hubble.Endpoint{Namespace: "default", Pod: "web-def456"},
		},
	}
}

func tcpLayer4() *flowpb.Layer4 {
	return &flowpb.Layer4{Protocol: &flowpb.Layer4_TCP{TCP: &flowpb.TCP{}}}
}

func TestProtocolOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		layer *flowpb.Layer4
		want  string
	}{
		{name: "nil layer4", layer: nil, want: ""},
		{name: "empty layer4", layer: &flowpb.Layer4{}, want: ""},
		{name: "tcp", layer: tcpLayer4(), want: "TCP"},
		{
			name:  "udp",
			layer: &flowpb.Layer4{Protocol: &flowpb.Layer4_UDP{UDP: &flowpb.UDP{}}},
			want:  "UDP",
		},
		{
			name:  "icmpv4",
			layer: &flowpb.Layer4{Protocol: &flowpb.Layer4_ICMPv4{ICMPv4: &flowpb.ICMPv4{}}},
			want:  "ICMPv4",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, hubble.ExportProtocolOf(test.layer))
		})
	}
}

func TestRecordFromFlow(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, time.June, 29, 8, 30, 0, 0, time.UTC)
	observed := &flowpb.Flow{
		Time:        timestamppb.New(when),
		Verdict:     flowpb.Verdict_FORWARDED,
		L4:          tcpLayer4(),
		Source:      &flowpb.Endpoint{Namespace: "default", PodName: "web-abc123"},
		Destination: &flowpb.Endpoint{Namespace: "kube-system", PodName: "coredns-xyz"},
	}

	record := hubble.ExportRecordFromFlow(observed)

	require.NotNil(t, record.Time)
	assert.True(t, when.Equal(*record.Time))
	assert.Equal(t, "FORWARDED", record.Verdict)
	assert.Equal(t, "TCP", record.Protocol)
	assert.Equal(t, hubble.Endpoint{Namespace: "default", Pod: "web-abc123"}, record.Source)
	assert.Equal(
		t,
		hubble.Endpoint{Namespace: "kube-system", Pod: "coredns-xyz"},
		record.Destination,
	)
}

func TestRecordFromFlowToleratesNilSubmessages(t *testing.T) {
	t.Parallel()

	record := hubble.ExportRecordFromFlow(&flowpb.Flow{})

	assert.Nil(t, record.Time)
	assert.Equal(t, "VERDICT_UNKNOWN", record.Verdict)
	assert.Empty(t, record.Protocol)
	assert.Equal(t, hubble.Endpoint{}, record.Source)
	assert.Equal(t, hubble.Endpoint{}, record.Destination)
}

func TestFilterFlows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     hubble.FilterOptions
		wantPods []string // source pods of the kept records, in order
	}{
		{
			name:     "no filter keeps all",
			opts:     hubble.FilterOptions{},
			wantPods: []string{"web-abc123", "prom-1"},
		},
		{
			name:     "namespace src or dst",
			opts:     hubble.FilterOptions{Namespace: "default"},
			wantPods: []string{"web-abc123", "prom-1"},
		},
		{
			name:     "namespace no match",
			opts:     hubble.FilterOptions{Namespace: "nope"},
			wantPods: []string{},
		},
		{
			name:     "pod substring src",
			opts:     hubble.FilterOptions{Pod: "web-abc"},
			wantPods: []string{"web-abc123"},
		},
		{
			name:     "pod substring dst",
			opts:     hubble.FilterOptions{Pod: "coredns"},
			wantPods: []string{"web-abc123"},
		},
		{
			name:     "protocol case-insensitive",
			opts:     hubble.FilterOptions{Protocol: "tcp"},
			wantPods: []string{"web-abc123"},
		},
		{
			name:     "combined ANDed",
			opts:     hubble.FilterOptions{Namespace: "default", Protocol: "udp"},
			wantPods: []string{"prom-1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := hubble.FilterFlows(sampleRecords(), test.opts)

			pods := make([]string, 0, len(got))
			for _, record := range got {
				pods = append(pods, record.Source.Pod)
			}

			assert.Equal(t, test.wantPods, pods)
		})
	}
}

func TestFilterFlowsDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	input := sampleRecords()
	_ = hubble.FilterFlows(input, hubble.FilterOptions{Namespace: "default"})

	assert.Len(t, input, 2)
}

func TestFormatJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, hubble.FormatJSON(&buf, sampleRecords()))

	var decoded []hubble.FlowRecord

	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded, 2)
	assert.Equal(t, "web-abc123", decoded[0].Source.Pod)
	assert.Equal(t, "DROPPED", decoded[1].Verdict)
}

func TestFormatJSONEmptyIsArray(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, hubble.FormatJSON(&buf, nil))
	assert.Equal(t, "[]\n", buf.String())
}

func TestFormatJSONOmitsMissingTime(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	record := hubble.FlowRecord{Verdict: "DROPPED", Source: hubble.Endpoint{Pod: "p"}}
	require.NoError(t, hubble.FormatJSON(&buf, []hubble.FlowRecord{record}))

	// A nil timestamp must not serialize as the year-0001 zero value.
	assert.NotContains(t, buf.String(), "0001-01-01")
	assert.NotContains(t, buf.String(), `"time"`)
}

func TestFormatPlain(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, hubble.FormatPlain(&buf, sampleRecords()))

	out := buf.String()
	assert.Contains(t, out, "TIME")
	assert.Contains(t, out, "default/web-abc123")
	assert.Contains(t, out, "kube-system/coredns-xyz")
	assert.Contains(t, out, "DROPPED")
}

func TestFormatPlainEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, hubble.FormatPlain(&buf, nil))
	assert.Equal(t, "No flows observed.\n", buf.String())
}

type fakeObserver struct {
	records  []hubble.FlowRecord
	err      error
	lastSeen uint64
	called   bool
}

func (f *fakeObserver) ObserveFlows(_ context.Context, last uint64) ([]hubble.FlowRecord, error) {
	f.called = true
	f.lastSeen = last

	return f.records, f.err
}

func (f *fakeObserver) StreamFlows(
	_ context.Context,
	last uint64,
	emit func(hubble.FlowRecord) error,
) error {
	f.called = true
	f.lastSeen = last

	for _, record := range f.records {
		emitErr := emit(record)
		if emitErr != nil {
			return emitErr
		}
	}

	return f.err
}

func TestObserveFiltersAndFormatsJSON(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Observe(context.Background(), observer, hubble.Options{
		Last:   50,
		Output: hubble.OutputJSON,
		Filter: hubble.FilterOptions{Protocol: "tcp"},
	}, &buf)
	require.NoError(t, err)
	assert.Equal(t, uint64(50), observer.lastSeen)

	var decoded []hubble.FlowRecord

	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	require.Len(t, decoded, 1)
	assert.Equal(t, "web-abc123", decoded[0].Source.Pod)
}

func TestObserveDefaultsToPlain(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := hubble.Observe(
		context.Background(),
		&fakeObserver{records: sampleRecords()},
		hubble.Options{},
		&buf,
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "TIME")
}

func TestObservePropagatesObserverError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := hubble.Observe(
		context.Background(),
		&fakeObserver{err: errObserve},
		hubble.Options{},
		&buf,
	)
	require.ErrorIs(t, err, errObserve)
}

func TestObserveRejectsUnknownOutputBeforeDialing(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Observe(context.Background(), observer, hubble.Options{Output: "yaml"}, &buf)
	require.ErrorIs(t, err, hubble.ErrUnknownOutputFormat)
	// The format is validated before any flows are fetched.
	assert.False(t, observer.called)
}

func TestStreamPlainWritesHeaderThenPerFlowRows(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Stream(context.Background(), observer, hubble.Options{Last: 7}, &buf)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), observer.lastSeen, "Last should be passed as the backlog size")

	lines := nonEmptyLines(buf.String())
	require.Len(t, lines, 3, "one header row plus one row per flow")
	assert.Contains(t, lines[0], "TIME")
	assert.Contains(t, lines[0], "VERDICT")
	assert.Contains(t, lines[1], "default/web-abc123")
	assert.Contains(t, lines[1], "FORWARDED")
	assert.Contains(t, lines[2], "monitoring/prom-1")
	assert.Contains(t, lines[2], "DROPPED")
}

func TestStreamJSONWritesNewlineDelimitedFlows(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Stream(
		context.Background(),
		observer,
		hubble.Options{Output: hubble.OutputJSON},
		&buf,
	)
	require.NoError(t, err)

	lines := nonEmptyLines(buf.String())
	require.Len(t, lines, 2, "NDJSON: one object per line, no array wrapper")

	for _, line := range lines {
		var record hubble.FlowRecord

		require.NoError(t, json.Unmarshal([]byte(line), &record), "each line is a JSON object")
	}

	assert.NotContains(t, buf.String(), "[", "NDJSON must not wrap the stream in an array")
}

func TestStreamAppliesFilterWhileStreaming(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Stream(context.Background(), observer, hubble.Options{
		Output: hubble.OutputJSON,
		Filter: hubble.FilterOptions{Protocol: "tcp"},
	}, &buf)
	require.NoError(t, err)

	lines := nonEmptyLines(buf.String())
	require.Len(t, lines, 1, "only the single TCP flow survives the filter")
	assert.Contains(t, lines[0], "web-abc123")
}

func TestStreamPropagatesObserverError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := hubble.Stream(
		context.Background(),
		&fakeObserver{err: errObserve},
		hubble.Options{},
		&buf,
	)
	require.ErrorIs(t, err, errObserve)
}

func TestStreamRejectsUnknownOutputBeforeStreaming(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Stream(context.Background(), observer, hubble.Options{Output: "yaml"}, &buf)
	require.ErrorIs(t, err, hubble.ErrUnknownOutputFormat)
	assert.False(t, observer.called, "format is validated before any flows are streamed")
}

func TestReceiveFlowsStopsOnEOFAndSkipsNilFlows(t *testing.T) {
	t.Parallel()

	flow := &flowpb.Flow{Verdict: flowpb.Verdict_FORWARDED}
	stream := &scriptedStream{steps: []recvStep{
		{flow: flow},
		{flow: nil}, // a non-flow response (e.g. node status) is skipped
		{flow: flow},
		{err: io.EOF},
	}}

	var got []hubble.FlowRecord

	err := hubble.ExportReceiveFlows(stream, func(record hubble.FlowRecord) error {
		got = append(got, record)

		return nil
	})
	require.NoError(t, err)
	assert.Len(t, got, 2, "EOF stops the loop and the nil-flow response is skipped")
}

func TestReceiveFlowsPropagatesEmitError(t *testing.T) {
	t.Parallel()

	stream := &scriptedStream{steps: []recvStep{
		{flow: &flowpb.Flow{}},
		{flow: &flowpb.Flow{}},
	}}

	calls := 0

	err := hubble.ExportReceiveFlows(stream, func(hubble.FlowRecord) error {
		calls++

		return errEmit
	})
	require.ErrorIs(t, err, errEmit)
	assert.Equal(t, 1, calls, "an emit error stops the loop immediately")
}

func TestReceiveFlowsPropagatesRecvError(t *testing.T) {
	t.Parallel()

	stream := &scriptedStream{steps: []recvStep{{err: errObserve}}}

	err := hubble.ExportReceiveFlows(stream, func(hubble.FlowRecord) error {
		return nil
	})
	require.ErrorIs(t, err, errObserve)
}

// TestReceiveFlowsEmptyStream verifies that a stream that immediately sends EOF
// yields zero records and returns nil (clean empty stream, not an error).
func TestReceiveFlowsEmptyStream(t *testing.T) {
	t.Parallel()

	stream := &scriptedStream{steps: []recvStep{{err: io.EOF}}}

	var called int

	err := hubble.ExportReceiveFlows(stream, func(hubble.FlowRecord) error {
		called++

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 0, called, "emit must never be called when the stream is immediately closed")
}

// TestStreamPlainNoFlows verifies that Stream with an empty record set still
// writes the header row and then nothing else — the header is always printed
// for plain mode regardless of whether any flows arrive.
func TestStreamPlainNoFlows(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: []hubble.FlowRecord{}}

	var buf bytes.Buffer

	err := hubble.Stream(context.Background(), observer, hubble.Options{}, &buf)
	require.NoError(t, err)

	lines := nonEmptyLines(buf.String())
	require.Len(t, lines, 1, "only the header row is written when there are no flows")
	assert.Contains(t, lines[0], "TIME")
	assert.Contains(t, lines[0], "SOURCE")
	assert.Contains(t, lines[0], "DESTINATION")
}

// TestStreamPlainAllFlowsFiltered verifies that when the filter drops every
// flow the header is still printed but no data rows appear.
func TestStreamPlainAllFlowsFiltered(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	err := hubble.Stream(context.Background(), observer, hubble.Options{
		Filter: hubble.FilterOptions{Namespace: "nonexistent"},
	}, &buf)
	require.NoError(t, err)

	lines := nonEmptyLines(buf.String())
	require.Len(t, lines, 1, "header plus zero data rows when all flows are filtered out")
	assert.Contains(t, lines[0], "TIME")
}

// TestStreamJSONNoFlows verifies that JSON stream mode with an empty record set
// produces no output at all — NDJSON has no array wrapper to emit for zero records.
func TestStreamJSONNoFlows(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: []hubble.FlowRecord{}}

	var buf bytes.Buffer

	err := hubble.Stream(
		context.Background(),
		observer,
		hubble.Options{Output: hubble.OutputJSON},
		&buf,
	)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(buf.String()), "NDJSON with zero flows must produce no output")
}

// TestStreamPlainFilterPartialMatch verifies that Stream passes only the matching
// flow rows through — a namespace filter that matches one of two flows yields
// exactly one data row (plus the header).
func TestStreamPlainFilterPartialMatch(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: sampleRecords()}

	var buf bytes.Buffer

	// "monitoring" matches the source of the second sample record only.
	err := hubble.Stream(context.Background(), observer, hubble.Options{
		Filter: hubble.FilterOptions{Namespace: "monitoring"},
	}, &buf)
	require.NoError(t, err)

	lines := nonEmptyLines(buf.String())
	require.Len(t, lines, 2, "header plus the single matching data row")
	assert.Contains(t, lines[1], "monitoring/prom-1")
}

// TestStreamPassesLastToObserver verifies that Stream forwards the Last backlog
// count to the underlying StreamFlows call unchanged, so the relay returns the
// correct number of historical flows before switching to live mode.
func TestStreamPassesLastToObserver(t *testing.T) {
	t.Parallel()

	observer := &fakeObserver{records: []hubble.FlowRecord{}}

	var buf bytes.Buffer

	err := hubble.Stream(
		context.Background(),
		observer,
		hubble.Options{Last: 42, Output: hubble.OutputJSON},
		&buf,
	)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), observer.lastSeen)
}

// nonEmptyLines splits rendered output into its non-blank lines.
func nonEmptyLines(out string) []string {
	var lines []string

	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

// recvStep is one scripted result from a [scriptedStream.Recv] call: either a
// flow (wrapped in a response) or a terminal error.
type recvStep struct {
	flow *flowpb.Flow
	err  error
}

// scriptedStream is a fake Hubble receive stream that replays scripted steps,
// so the relay receive loop can be tested without a live relay.
type scriptedStream struct {
	steps []recvStep
	pos   int
}

func (s *scriptedStream) Recv() (*observerpb.GetFlowsResponse, error) {
	if s.pos >= len(s.steps) {
		return nil, io.EOF
	}

	step := s.steps[s.pos]
	s.pos++

	if step.err != nil {
		return nil, step.err
	}

	return &observerpb.GetFlowsResponse{
		ResponseTypes: &observerpb.GetFlowsResponse_Flow{Flow: step.flow},
	}, nil
}
