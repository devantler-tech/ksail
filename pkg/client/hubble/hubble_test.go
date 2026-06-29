package hubble_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	flowpb "github.com/cilium/cilium/api/v1/flow"
	"github.com/devantler-tech/ksail/v7/pkg/client/hubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errObserve = errors.New("boom")

func sampleRecords() []hubble.FlowRecord {
	base := time.Date(2026, time.June, 29, 8, 0, 0, 0, time.UTC)

	return []hubble.FlowRecord{
		{
			Time:        base,
			Verdict:     "FORWARDED",
			Protocol:    "TCP",
			Source:      hubble.Endpoint{Namespace: "default", Pod: "web-abc123"},
			Destination: hubble.Endpoint{Namespace: "kube-system", Pod: "coredns-xyz"},
		},
		{
			Time:        base.Add(time.Second),
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

	assert.True(t, when.Equal(record.Time))
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

	assert.True(t, record.Time.IsZero())
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
}

func (f *fakeObserver) ObserveFlows(_ context.Context, last uint64) ([]hubble.FlowRecord, error) {
	f.lastSeen = last

	return f.records, f.err
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

func TestObserveRejectsUnknownOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := hubble.Observe(
		context.Background(),
		&fakeObserver{records: sampleRecords()},
		hubble.Options{Output: "yaml"},
		&buf,
	)
	require.ErrorIs(t, err, hubble.ErrUnknownOutputFormat)
}
