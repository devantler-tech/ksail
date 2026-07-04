package mirror_test

import (
	"bytes"
	"context"
	"io"
	"net"
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

// replayServicePort is the mirrored service port the test packets target.
const replayServicePort = 8080

// replayClientPort is the mirrored client's source port in single-flow tests.
const replayClientPort = 40001

// collectTimeout bounds how long a test waits for a replayed connection's
// bytes to arrive.
const collectTimeout = 5 * time.Second

// pcapSnapLen is the snapshot length written to the synthetic pcap header.
const pcapSnapLen = 65535

// replayCollector receives the connections ReplayCapture dials and records
// what each one delivered.
type replayCollector struct {
	listener net.Listener
	payloads chan string
}

// newReplayCollector starts a loopback listener whose accepted connections
// each report their full received byte stream on the payloads channel once
// the peer closes.
func newReplayCollector(t *testing.T) *replayCollector {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	collector := &replayCollector{
		listener: listener,
		payloads: make(chan string, 16),
	}

	go collector.acceptLoop()

	t.Cleanup(func() { _ = listener.Close() })

	return collector
}

// addr returns the listener's dialable address.
func (c *replayCollector) addr() string {
	return c.listener.Addr().String()
}

// acceptLoop drains accepted connections into the payloads channel. The
// preflight connection closes without sending anything and surfaces as an
// empty string, which tests skip over.
func (c *replayCollector) acceptLoop() {
	var waitGroup sync.WaitGroup

	for {
		conn, err := c.listener.Accept()
		if err != nil {
			waitGroup.Wait()

			return
		}

		waitGroup.Go(func() {
			data, _ := io.ReadAll(conn)
			_ = conn.Close()

			c.payloads <- string(data)
		})
	}
}

// waitPayload returns the next non-empty received stream (skipping the
// preflight connection's empty one), or fails the test on timeout.
func (c *replayCollector) waitPayload(t *testing.T) string {
	t.Helper()

	deadline := time.After(collectTimeout)

	for {
		select {
		case payload := <-c.payloads:
			if payload == "" {
				continue
			}

			return payload
		case <-deadline:
			t.Fatal("timed out waiting for a replayed payload")

			return ""
		}
	}
}

// tcpSegment describes one synthetic captured segment.
type tcpSegment struct {
	srcPort uint16
	dstPort uint16
	seq     uint32
	syn     bool
	fin     bool
	payload string
}

// buildPcap serializes the segments into a complete pcap byte stream as the
// tap's tcpdump would produce it.
func buildPcap(t *testing.T, segments ...tcpSegment) []byte {
	t.Helper()

	var buffer bytes.Buffer

	writer := pcapgo.NewWriter(&buffer)
	err := writer.WriteFileHeader(pcapSnapLen, layers.LinkTypeEthernet)
	require.NoError(t, err)

	for _, segment := range segments {
		data := serializeSegment(t, segment)
		info := gopacket.CaptureInfo{
			Timestamp:     time.Now(),
			CaptureLength: len(data),
			Length:        len(data),
		}

		err = writer.WritePacket(info, data)
		require.NoError(t, err)
	}

	return buffer.Bytes()
}

// serializeSegment builds one Ethernet/IPv4/TCP packet.
func serializeSegment(t *testing.T, segment tcpSegment) []byte {
	t.Helper()

	ethernet := layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01},
		DstMAC:       net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x02},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ipv4 := layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    net.IPv4(10, 0, 0, 1),
		DstIP:    net.IPv4(10, 0, 0, 2),
	}
	tcp := layers.TCP{
		SrcPort: layers.TCPPort(segment.srcPort),
		DstPort: layers.TCPPort(segment.dstPort),
		Seq:     segment.seq,
		SYN:     segment.syn,
		FIN:     segment.fin,
		Window:  pcapSnapLen,
	}

	err := tcp.SetNetworkLayerForChecksum(&ipv4)
	require.NoError(t, err)

	buffer := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}

	err = gopacket.SerializeLayers(
		buffer, options, &ethernet, &ipv4, &tcp, gopacket.Payload(segment.payload),
	)
	require.NoError(t, err)

	return buffer.Bytes()
}

// inboundSegment is a payload segment from the mirrored client to the service
// port.
func inboundSegment(seq uint32, payload string) tcpSegment {
	return tcpSegment{
		srcPort: replayClientPort,
		dstPort: replayServicePort,
		seq:     seq,
		syn:     false,
		fin:     false,
		payload: payload,
	}
}

// clientInitialSeq is the mirrored client's initial sequence number in
// single-flow tests.
const clientInitialSeq = 100

// synSegment starts the mirrored client's connection at clientInitialSeq.
func synSegment() tcpSegment {
	return tcpSegment{
		srcPort: replayClientPort,
		dstPort: replayServicePort,
		seq:     clientInitialSeq,
		syn:     true,
		fin:     false,
		payload: "",
	}
}

func TestReplayCapture_NilStream(t *testing.T) {
	t.Parallel()

	err := mirror.ReplayCapture(t.Context(), nil, replayServicePort, "127.0.0.1:1")

	require.ErrorIs(t, err, mirror.ErrReplayStreamNil)
}

func TestReplayCapture_InvalidPort(t *testing.T) {
	t.Parallel()

	err := mirror.ReplayCapture(t.Context(), bytes.NewReader(nil), 0, "127.0.0.1:1")

	require.ErrorIs(t, err, mirror.ErrInvalidCapturePort)
}

func TestReplayCapture_EmptyAddress(t *testing.T) {
	t.Parallel()

	err := mirror.ReplayCapture(t.Context(), bytes.NewReader(nil), replayServicePort, "")

	require.ErrorIs(t, err, mirror.ErrReplayAddressEmpty)
}

func TestReplayCapture_InvalidAddress(t *testing.T) {
	t.Parallel()

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(nil), replayServicePort, "no-port-here",
	)

	require.ErrorIs(t, err, mirror.ErrReplayAddressInvalid)
}

func TestReplayCapture_UnreachableAddress(t *testing.T) {
	t.Parallel()

	// Grab a loopback port that is guaranteed closed by opening and closing a
	// listener on it.
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	closedAddr := listener.Addr().String()
	require.NoError(t, listener.Close())

	err = mirror.ReplayCapture(
		t.Context(), bytes.NewReader(nil), replayServicePort, closedAddr,
	)

	require.ErrorIs(t, err, mirror.ErrReplayUnreachable)
}

func TestReplayCapture_MalformedStream(t *testing.T) {
	t.Parallel()

	collector := newReplayCollector(t)

	err := mirror.ReplayCapture(
		t.Context(),
		bytes.NewReader([]byte("this is not a pcap stream")),
		replayServicePort,
		collector.addr(),
	)

	require.ErrorContains(t, err, "read pcap header")
}

func TestReplayCapture_CancelledContextIsCleanStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := mirror.ReplayCapture(
		ctx, bytes.NewReader(nil), replayServicePort, "127.0.0.1:1",
	)

	require.NoError(t, err)
}

func TestReplayCapture_ReplaysInOrderPayloads(t *testing.T) {
	t.Parallel()

	collector := newReplayCollector(t)
	stream := buildPcap(t,
		synSegment(),
		inboundSegment(101, "hello "),
		inboundSegment(107, "world"),
	)

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(stream), replayServicePort, collector.addr(),
	)

	require.NoError(t, err)
	assert.Equal(t, "hello world", collector.waitPayload(t))
}

func TestReplayCapture_ReordersOutOfOrderSegments(t *testing.T) {
	t.Parallel()

	collector := newReplayCollector(t)
	stream := buildPcap(t,
		synSegment(),
		inboundSegment(107, "world"),
		inboundSegment(101, "hello "),
	)

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(stream), replayServicePort, collector.addr(),
	)

	require.NoError(t, err)
	assert.Equal(t, "hello world", collector.waitPayload(t))
}

func TestReplayCapture_DropsRetransmissionsAndTrimsOverlap(t *testing.T) {
	t.Parallel()

	collector := newReplayCollector(t)
	stream := buildPcap(t,
		synSegment(),
		inboundSegment(101, "hello "),
		// Pure retransmission — already delivered, dropped.
		inboundSegment(101, "hello "),
		// Overlapping retransmission — only the unseen suffix is delivered.
		inboundSegment(101, "hello world"),
	)

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(stream), replayServicePort, collector.addr(),
	)

	require.NoError(t, err)
	assert.Equal(t, "hello world", collector.waitPayload(t))
}

func TestReplayCapture_JoinsEstablishedFlowMidStream(t *testing.T) {
	t.Parallel()

	collector := newReplayCollector(t)
	// No SYN in the capture — the tap attached after the connection opened.
	stream := buildPcap(t,
		inboundSegment(500, "mid-stream "),
		inboundSegment(511, "join"),
	)

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(stream), replayServicePort, collector.addr(),
	)

	require.NoError(t, err)
	assert.Equal(t, "mid-stream join", collector.waitPayload(t))
}

func TestReplayCapture_IgnoresOtherPortsAndResponses(t *testing.T) {
	t.Parallel()

	collector := newReplayCollector(t)
	stream := buildPcap(t,
		// The workload's response direction: src is the service port.
		tcpSegment{
			srcPort: replayServicePort,
			dstPort: replayClientPort,
			seq:     900,
			syn:     false,
			fin:     false,
			payload: "response body",
		},
		// Traffic on an unrelated port.
		tcpSegment{
			srcPort: replayClientPort,
			dstPort: replayServicePort + 1,
			seq:     100,
			syn:     false,
			fin:     false,
			payload: "other port",
		},
	)

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(stream), replayServicePort, collector.addr(),
	)
	require.NoError(t, err)

	select {
	case payload := <-collector.payloads:
		assert.Empty(t, payload, "only the preflight connection should have connected")
	default:
	}
}

func TestReplayCapture_ReplaysConcurrentFlowsSeparately(t *testing.T) {
	t.Parallel()

	const secondClientPort = replayClientPort + 1

	collector := newReplayCollector(t)
	stream := buildPcap(t,
		synSegment(),
		tcpSegment{
			srcPort: secondClientPort,
			dstPort: replayServicePort,
			seq:     200,
			syn:     true,
			fin:     false,
			payload: "",
		},
		inboundSegment(101, "flow one"),
		tcpSegment{
			srcPort: secondClientPort,
			dstPort: replayServicePort,
			seq:     201,
			syn:     false,
			fin:     false,
			payload: "flow two",
		},
	)

	err := mirror.ReplayCapture(
		t.Context(), bytes.NewReader(stream), replayServicePort, collector.addr(),
	)
	require.NoError(t, err)

	received := []string{collector.waitPayload(t), collector.waitPayload(t)}

	assert.ElementsMatch(t, []string{"flow one", "flow two"}, received)
}
