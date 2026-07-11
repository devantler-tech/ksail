package mirror_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	replayTestPort    = 8080
	replayTestAddress = "localhost:9999"
)

// errReplayDialRefused is the static dial failure the replay tests inject.
var errReplayDialRefused = errors.New("connection refused")

// replaySegment describes one synthetic TCP segment of a test capture stream.
type replaySegment struct {
	srcPort int
	dstPort int
	seq     uint32
	syn     bool
	fin     bool
	payload []byte
}

// serializeTCP builds the raw IPv4+TCP bytes for a segment.
func serializeTCP(t *testing.T, segment replaySegment) []byte {
	t.Helper()

	ipLayer := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    net.IPv4(10, 0, 0, 1),
		DstIP:    net.IPv4(10, 0, 0, 2),
	}

	tcpLayer := &layers.TCP{
		SrcPort: layers.TCPPort(segment.srcPort), //nolint:gosec // G115: test ports fit uint16.
		DstPort: layers.TCPPort(segment.dstPort), //nolint:gosec // G115: test ports fit uint16.
		Seq:     segment.seq,
		SYN:     segment.syn,
		FIN:     segment.fin,
		Window:  65535,
	}
	require.NoError(t, tcpLayer.SetNetworkLayerForChecksum(ipLayer))

	buffer := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}

	require.NoError(t, gopacket.SerializeLayers(
		buffer, options, ipLayer, tcpLayer, gopacket.Payload(segment.payload),
	))

	return buffer.Bytes()
}

// sll2Packet wraps raw IP bytes in a LINUX_SLL2 header — the link type the
// tap's "-i any" capture produces.
func sll2Packet(ipBytes []byte) []byte {
	packet := make([]byte, 20+len(ipBytes))
	binary.BigEndian.PutUint16(packet[0:2], 0x0800) // EtherType: IPv4
	binary.BigEndian.PutUint16(packet[8:10], 1)     // ARPHRD_ETHER
	packet[11] = 6                                  // address length
	copy(packet[20:], ipBytes)

	return packet
}

// buildReplayPcap serializes segments into a LINUX_SLL2 pcap stream, matching
// what the tap's tcpdump writes over the exec channel.
func buildReplayPcap(t *testing.T, segments ...replaySegment) []byte {
	t.Helper()

	var buffer bytes.Buffer

	writer := pcapgo.NewWriter(&buffer)
	require.NoError(t, writer.WriteFileHeader(262144, layers.LinkTypeLinuxSLL2))

	for _, segment := range segments {
		packet := sll2Packet(serializeTCP(t, segment))
		info := gopacket.CaptureInfo{
			Timestamp:     time.Unix(0, 0),
			CaptureLength: len(packet),
			Length:        len(packet),
		}
		require.NoError(t, writer.WritePacket(info, packet))
	}

	return buffer.Bytes()
}

// recordingDialer returns a ReplayDialer handing out in-memory connections
// and a function that collects everything written to them, per dial order.
func recordingDialer(t *testing.T) (mirror.ReplayDialer, func() []string) {
	t.Helper()

	var (
		resultsMu sync.Mutex
		results   []*bytes.Buffer
		wait      sync.WaitGroup
	)

	dial := func(_, _ string) (net.Conn, error) {
		client, server := net.Pipe()

		buffer := &bytes.Buffer{}

		resultsMu.Lock()

		results = append(results, buffer)

		resultsMu.Unlock()

		wait.Go(func() {
			data, _ := io.ReadAll(server)

			resultsMu.Lock()
			buffer.Write(data)
			resultsMu.Unlock()
		})

		return client, nil
	}

	collect := func() []string {
		wait.Wait()

		resultsMu.Lock()
		defer resultsMu.Unlock()

		collected := make([]string, 0, len(results))
		for _, buffer := range results {
			collected = append(collected, buffer.String())
		}

		return collected
	}

	return dial, collect
}

// runReplay feeds a pcap stream through a LiveReplay built with the given
// dialer and returns Close's error.
func runReplay(t *testing.T, dial mirror.ReplayDialer, pcap []byte) error {
	t.Helper()

	replay, err := mirror.NewLiveReplay(
		replayTestAddress, replayTestPort, mirror.WithReplayDialer(dial),
	)
	require.NoError(t, err)

	_, writeErr := replay.Write(pcap)
	require.NoError(t, writeErr)

	closeErr := replay.Close()
	if closeErr != nil {
		return fmt.Errorf("close replay: %w", closeErr)
	}

	return nil
}

func TestLiveReplayDeliversInOrderPayloads(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	pcap := buildReplayPcap(
		t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("GET / ")},
		replaySegment{
			srcPort: 40000,
			dstPort: replayTestPort,
			seq:     107,
			payload: []byte("HTTP/1.1"),
		},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 115, fin: true},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Equal(t, []string{"GET / HTTP/1.1"}, collect())
}

func TestLiveReplayReordersOutOfOrderSegments(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	// The second segment arrives before the first: delivery must still be
	// in sequence order.
	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 107, payload: []byte("world")},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("hello ")},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Equal(t, []string{"hello world"}, collect())
}

func TestLiveReplayDropsRetransmissions(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("once")},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("once")},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Equal(t, []string{"once"}, collect())
}

func TestLiveReplaySeparatesFlows(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 41000, dstPort: replayTestPort, seq: 500, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("first")},
		replaySegment{srcPort: 41000, dstPort: replayTestPort, seq: 501, payload: []byte("second")},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.ElementsMatch(t, []string{"first", "second"}, collect())
}

func TestLiveReplayIgnoresResponsesAndOtherPorts(t *testing.T) {
	t.Parallel()

	dialed := 0
	dial := func(_, _ string) (net.Conn, error) {
		dialed++

		client, server := net.Pipe()

		go func() { _, _ = io.Copy(io.Discard, server) }()

		return client, nil
	}

	// A pod response (source port == service port) and traffic for another
	// port: neither is inbound for the mirrored port, so nothing dials.
	pcap := buildReplayPcap(t,
		replaySegment{srcPort: replayTestPort, dstPort: 40000, seq: 100, payload: []byte("resp")},
		replaySegment{srcPort: 40000, dstPort: 9090, seq: 100, payload: []byte("other")},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Zero(t, dialed, "responses and other ports must not be replayed")
}

func TestLiveReplayRecordsDialFailure(t *testing.T) {
	t.Parallel()

	dial := func(_, _ string) (net.Conn, error) { return nil, errReplayDialRefused }

	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("data")},
	)

	err := runReplay(t, dial, pcap)
	require.ErrorIs(t, err, errReplayDialRefused, "Close must surface the local dial failure")
}

func TestLiveReplayFailsWritesOnMalformedStream(t *testing.T) {
	t.Parallel()

	dial, _ := recordingDialer(t)

	replay, err := mirror.NewLiveReplay(
		replayTestAddress, replayTestPort, mirror.WithReplayDialer(dial),
	)
	require.NoError(t, err)

	garbage := bytes.Repeat([]byte("not a pcap stream "), 64)

	_, writeErr := replay.Write(garbage)
	if writeErr == nil {
		// The parser may consume the first chunk before failing; the next
		// write must observe the dead parser.
		_, writeErr = replay.Write(garbage)
	}

	require.Error(t, writeErr, "writes must fail once the parser stopped on a malformed stream")

	_ = replay.Close()
}

func TestLiveReplayHoldsFinUntilGapFills(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	// The FIN arrives before the payload that precedes it in sequence order:
	// the flow must stay open until the gap fills, then deliver and finish.
	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 113, fin: true},
		replaySegment{
			srcPort: 40000,
			dstPort: replayTestPort,
			seq:     101,
			payload: []byte("hello world!"),
		},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Equal(t, []string{"hello world!"}, collect())
}

func TestLiveReplayFlushesOverlappingPendingTail(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	// The buffered segment (seq 107, "world") starts BEFORE where the
	// in-order segment (seq 101, "hello wor") advances the stream to (110):
	// its still-new tail ("ld") must be delivered, not stranded.
	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 107, payload: []byte("world")},
		replaySegment{
			srcPort: 40000,
			dstPort: replayTestPort,
			seq:     101,
			payload: []byte("hello wor"),
		},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Equal(t, []string{"hello world"}, collect())
}

func TestLiveReplayKeepsLongerBufferedSegmentOverShorterDuplicate(t *testing.T) {
	t.Parallel()

	dial, collect := recordingDialer(t)

	// A shorter retransmission of an already-buffered segment must not
	// replace the longer copy — its tail would be lost.
	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 107, payload: []byte("world")},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 107, payload: []byte("wor")},
		replaySegment{
			srcPort: 40000,
			dstPort: replayTestPort,
			seq:     101,
			payload: []byte("hello wor"),
		},
	)

	require.NoError(t, runReplay(t, dial, pcap))
	assert.Equal(t, []string{"hello world"}, collect())
}

func TestLiveReplaySurfacesTruncatedCapture(t *testing.T) {
	t.Parallel()

	dial, _ := recordingDialer(t)

	pcap := buildReplayPcap(t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 101, payload: []byte("data")},
	)

	// Cut into the last packet record: a capture that ends mid-packet is
	// truncated input, not a clean shutdown.
	err := runReplay(t, dial, pcap[:len(pcap)-5])
	require.ErrorIs(t, err, mirror.ErrReplayTruncatedCapture)
}

func TestLiveReplayTimesOutStalledLocalWrites(t *testing.T) {
	t.Parallel()

	// A local process that accepts the connection but never reads: without
	// a write deadline the parser goroutine would block forever.
	dial := func(_, _ string) (net.Conn, error) {
		client, _ := net.Pipe()

		return client, nil
	}

	replay, err := mirror.NewLiveReplay(
		replayTestAddress,
		replayTestPort,
		mirror.WithReplayDialer(dial),
		mirror.WithReplayWriteTimeout(50*time.Millisecond),
	)
	require.NoError(t, err)

	pcap := buildReplayPcap(
		t,
		replaySegment{srcPort: 40000, dstPort: replayTestPort, seq: 100, syn: true},
		replaySegment{
			srcPort: 40000,
			dstPort: replayTestPort,
			seq:     101,
			payload: []byte("stalled"),
		},
	)

	_, writeErr := replay.Write(pcap)
	require.NoError(t, writeErr)

	require.ErrorIs(t, replay.Close(), os.ErrDeadlineExceeded)
}

func TestLiveReplayValidatesArguments(t *testing.T) {
	t.Parallel()

	_, err := mirror.NewLiveReplay("", replayTestPort)
	require.ErrorIs(t, err, mirror.ErrReplayAddressEmpty)

	_, err = mirror.NewLiveReplay("no-port", replayTestPort)
	require.Error(t, err)

	_, err = mirror.NewLiveReplay(replayTestAddress, 0)
	require.ErrorIs(t, err, mirror.ErrInvalidCapturePort)
}
