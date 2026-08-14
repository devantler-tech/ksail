package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// ErrTunnelSessionClosed is returned when a stream is opened or accepted on a
// session that is already closed, and is the read error surfaced on streams a
// session close tore down.
var ErrTunnelSessionClosed = errors.New("tunnel session is closed")

// ErrTunnelStreamClosed is returned when writing to (or reading from) a stream
// after its local side closed it.
var ErrTunnelStreamClosed = errors.New("tunnel stream is closed")

// ErrTunnelDuplicateOpen is returned when the peer announces a StreamID that
// is already open — a protocol violation that tears the session down, because
// frame routing for that StreamID is ambiguous from that point on.
var ErrTunnelDuplicateOpen = errors.New("tunnel peer opened an already-open stream")

// TunnelRole fixes which half of the StreamID space a session allocates from,
// so the two ends of a tunnel can both open streams without ever colliding.
type TunnelRole uint8

const (
	// TunnelRoleClient allocates odd StreamIDs (1, 3, 5, …) — the local ksail
	// end of the tunnel.
	TunnelRoleClient TunnelRole = iota
	// TunnelRoleServer allocates even StreamIDs (2, 4, 6, …) — the in-cluster
	// end of the tunnel.
	TunnelRoleServer
)

// streamIDStep keeps each side on its own StreamID parity when allocating.
const streamIDStep = 2

// acceptBacklog bounds how many peer-opened streams may sit undelivered
// before the demux loop blocks waiting for AcceptStream — deliberate
// backpressure on the single shared channel rather than an unbounded queue.
const acceptBacklog = 8

// tunnelReceiveBudget bounds how many inbound bytes one stream may buffer
// before the demux loop blocks (backpressure on the whole tunnel, since all
// streams share the one channel). The slack keeps ordinary
// write-then-close sequences deadlock-free without unbounded memory;
// windowed flow control is a later increment if intercept needs it.
const tunnelReceiveBudget = 4 * MaxTunnelPayload

// TunnelSession multiplexes one bidirectional byte channel — in Phase 2 the
// exec stream's stdin+stdout — into independent per-connection streams using
// the tunnel frame codec ([Frame], [WriteFrame], [ReadFrame]). Both ends may
// open streams; [TunnelRole] parity keeps their StreamIDs disjoint.
//
// Inbound data is delivered through one bounded buffer per stream; a stream
// left unread beyond its budget blocks the shared demux loop. That
// head-of-line blocking is the deliberate backpressure of a single shared
// channel (windowed flow control is a later increment if intercept needs it).
type TunnelSession struct {
	reader io.Reader
	writer io.Writer

	writeMu sync.Mutex

	acceptQueue chan *TunnelStream

	stateMu sync.Mutex
	streams map[uint32]*TunnelStream
	nextID  uint32
	closed  bool
	loopErr error

	closeOnce sync.Once
	closing   chan struct{}
	done      chan struct{}

	// lastRead is the UnixNano timestamp of the most recent successfully
	// read frame (any type — data implies liveness as much as a keepalive).
	// The steering agent's liveness watchdog polls it through
	// [TunnelSession.LastRead] to detect a dead client (ksail#6040).
	lastRead atomic.Int64

	// keepaliveSeen flips once the session has received its first
	// [FrameKeepalive], or immediately when the composition pre-arms the
	// session via [TunnelSession.ArmLiveness] (the client declared it will
	// ping). The liveness watchdog only arms on that proof that the peer
	// speaks the keepalive protocol, so a pre-keepalive peer (e.g. an older
	// client execing into a reused agent container) is never expired for
	// not pinging — it keeps the pre-keepalive behaviour.
	keepaliveSeen atomic.Bool

	// dispatching is true while the demux loop is inside dispatch. A
	// dispatch may legitimately block for longer than the liveness timeout
	// (a stream past its receive budget parks the loop — the deliberate
	// head-of-line backpressure above), during which no further frames can
	// be read; the watchdog treats that as "cannot measure liveness", not
	// "peer is dead" ([TunnelSession.DispatchInProgress]).
	dispatching atomic.Bool
}

// NewTunnelSession starts a session over the given channel halves and begins
// demultiplexing immediately. For [TunnelSession.Close] to be able to unblock
// a demux loop parked in a read, reader and writer should implement
// [io.Closer] (both halves of an [io.Pipe] do, as do the exec stream's ends).
func NewTunnelSession(reader io.Reader, writer io.Writer, role TunnelRole) *TunnelSession {
	firstID := uint32(1)
	if role == TunnelRoleServer {
		firstID = 2
	}

	session := &TunnelSession{
		reader:      reader,
		writer:      writer,
		writeMu:     sync.Mutex{},
		acceptQueue: make(chan *TunnelStream, acceptBacklog),
		stateMu:     sync.Mutex{},
		streams:     map[uint32]*TunnelStream{},
		nextID:      firstID,
		closed:      false,
		loopErr:     nil,
		closeOnce:   sync.Once{},
		closing:     make(chan struct{}),
		done:        make(chan struct{}),
	}

	session.lastRead.Store(time.Now().UnixNano())

	go session.readLoop()

	return session
}

// LastRead reports when the session last successfully read a frame off the
// channel (any type). A session that has read nothing yet reports its
// creation time, so a liveness deadline measured from LastRead is armed from
// the start.
func (s *TunnelSession) LastRead() time.Time {
	return time.Unix(0, s.lastRead.Load())
}

// SendKeepalive writes one session-level liveness ping ([FrameKeepalive]) to
// the peer. The intercept client calls it on a timer so the steering agent's
// watchdog can distinguish an idle client from a dead one (ksail#6040).
func (s *TunnelSession) SendKeepalive() error {
	return s.writeFrame(Frame{StreamID: 0, Type: FrameKeepalive, Payload: nil})
}

// KeepaliveSeen reports whether the session's liveness watchdog is armed:
// the session has received at least one [FrameKeepalive] from its peer, or
// [TunnelSession.ArmLiveness] pre-armed it. The liveness watchdog uses it as
// the arming condition: only a peer that has proven — or whose client has
// declared — it speaks the keepalive protocol is ever expired for frame
// silence (see the keepaliveSeen field).
func (s *TunnelSession) KeepaliveSeen() bool {
	return s.keepaliveSeen.Load()
}

// ArmLiveness arms the liveness watchdog without waiting for the first
// [FrameKeepalive]. The steering agent calls it when the intercept client
// declared keepalive support up front (the --expect-keepalives agent flag,
// appended only after the client proved the agent image speaks the
// protocol): [TunnelSession.LastRead] starts at the session's creation time,
// so a client that dies before its very first ping is delivered still
// expires after the liveness timeout instead of orphaning the agent's
// REDIRECT rule (ksail#6040).
func (s *TunnelSession) ArmLiveness() {
	s.keepaliveSeen.Store(true)
}

// DispatchInProgress reports whether the demux loop is currently inside
// dispatch. While true, frame silence proves nothing about the peer — the
// loop cannot read the channel while a backpressured stream blocks it — so
// the liveness watchdog holds off instead of expiring the session (see the
// dispatching field).
func (s *TunnelSession) DispatchInProgress() bool {
	return s.dispatching.Load()
}

// OpenStream announces a new locally-initiated stream to the peer and returns
// it. The peer surfaces it through [TunnelSession.AcceptStream].
func (s *TunnelSession) OpenStream() (*TunnelStream, error) {
	s.stateMu.Lock()

	if s.closed {
		s.stateMu.Unlock()

		return nil, ErrTunnelSessionClosed
	}

	streamID := s.nextID
	s.nextID += streamIDStep
	stream := newTunnelStream(s, streamID)
	s.streams[streamID] = stream
	s.stateMu.Unlock()

	err := s.writeFrame(Frame{StreamID: streamID, Type: FrameOpen, Payload: nil})
	if err != nil {
		_ = stream.closeWith(false, ErrTunnelSessionClosed)

		return nil, err
	}

	return stream, nil
}

// AcceptStream returns the next peer-initiated stream, blocking until one
// arrives, the context is cancelled, or the session closes.
func (s *TunnelSession) AcceptStream(ctx context.Context) (*TunnelStream, error) {
	// Prefer an already-delivered stream over racing the done channel, so
	// streams the peer opened before a close are still handed out.
	select {
	case stream := <-s.acceptQueue:
		return stream, nil
	default:
	}

	select {
	case stream := <-s.acceptQueue:
		return stream, nil
	case <-s.done:
		return nil, ErrTunnelSessionClosed
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for a tunnel stream: %w", ctx.Err())
	}
}

// Close tears the session down: every open stream is closed (readers see
// ErrTunnelSessionClosed), the channel halves are closed when they implement
// [io.Closer], and the demux loop stops. Close never blocks and is safe to
// call more than once.
func (s *TunnelSession) Close() error {
	s.closeOnce.Do(func() {
		s.stateMu.Lock()
		s.closed = true
		s.stateMu.Unlock()

		close(s.closing)
		closeIfCloser(s.reader)
		closeIfCloser(s.writer)

		// Unblock a demux loop parked on an unread stream's pipe so it can
		// observe the closed reader and tear down.
		for _, stream := range s.snapshotStreams() {
			_ = stream.closeWith(false, ErrTunnelSessionClosed)
		}
	})

	return nil
}

// Done is closed once the demux loop has exited and every stream is torn
// down; [TunnelSession.Err] is meaningful after that.
func (s *TunnelSession) Done() <-chan struct{} {
	return s.done
}

// Err reports why the demux loop exited: nil for a clean shutdown (peer EOF
// or [TunnelSession.Close]), otherwise the codec/protocol/transport error
// that tore the session down.
func (s *TunnelSession) Err() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	return s.loopErr
}

// readLoop is the demux: it reads one frame at a time off the shared channel
// and routes it, exiting on EOF, a deliberate close, or the first error.
func (s *TunnelSession) readLoop() {
	var loopErr error

	for {
		frame, err := ReadFrame(s.reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !s.isClosing() {
				loopErr = err
			}

			break
		}

		s.lastRead.Store(time.Now().UnixNano())

		s.dispatching.Store(true)
		err = s.dispatch(frame)

		// Re-stamp before clearing the in-progress flag: a dispatch can
		// block past the liveness timeout (backpressure), and the watchdog
		// must never observe "not dispatching" together with the stale
		// pre-dispatch timestamp — completing a dispatch is channel
		// progress just like reading the frame was.
		s.lastRead.Store(time.Now().UnixNano())
		s.dispatching.Store(false)

		if err != nil {
			// A dispatch parked on the accept queue unblocks through the
			// closing channel during a deliberate Close; that is a clean
			// shutdown, not a loop failure.
			if !s.isClosing() {
				loopErr = err
			}

			break
		}
	}

	s.teardown(loopErr)
}

// dispatch routes one inbound frame by type. ReadFrame already rejected
// unknown types, so the default arm is defensive only.
func (s *TunnelSession) dispatch(frame Frame) error {
	switch frame.Type {
	case FrameOpen:
		return s.handleOpen(frame.StreamID)
	case FrameData:
		s.handleData(frame)

		return nil
	case FrameClose:
		s.handleClose(frame.StreamID)

		return nil
	case FrameKeepalive:
		// Liveness only: readLoop already stamped lastRead before
		// dispatching. Recording that the peer speaks the keepalive
		// protocol is what arms the liveness watchdog (KeepaliveSeen).
		s.keepaliveSeen.Store(true)

		return nil
	default:
		return fmt.Errorf("%w: %d", ErrTunnelUnknownFrameType, frame.Type)
	}
}

// handleOpen registers a peer-initiated stream and queues it for
// AcceptStream. A StreamID that is already open is a protocol violation that
// tears the session down.
func (s *TunnelSession) handleOpen(streamID uint32) error {
	s.stateMu.Lock()

	if s.closed {
		s.stateMu.Unlock()

		return ErrTunnelSessionClosed
	}

	_, exists := s.streams[streamID]
	if exists {
		s.stateMu.Unlock()

		return fmt.Errorf("%w: stream %d", ErrTunnelDuplicateOpen, streamID)
	}

	stream := newTunnelStream(s, streamID)
	s.streams[streamID] = stream
	s.stateMu.Unlock()

	select {
	case s.acceptQueue <- stream:
		return nil
	case <-s.closing:
		return ErrTunnelSessionClosed
	}
}

// handleData feeds a data frame into its stream's inbound buffer. Data for
// an unknown stream raced a close and is dropped, as is data arriving after
// the local side closed the stream mid-flight.
func (s *TunnelSession) handleData(frame Frame) {
	s.stateMu.Lock()
	stream, ok := s.streams[frame.StreamID]
	s.stateMu.Unlock()

	if !ok {
		return
	}

	stream.inbound.write(frame.Payload)
}

// handleClose tears down the stream the peer ended; its local reader drains
// buffered bytes and then sees io.EOF. A close for an unknown stream raced a
// local close and is dropped.
func (s *TunnelSession) handleClose(streamID uint32) {
	s.stateMu.Lock()
	stream, ok := s.streams[streamID]
	s.stateMu.Unlock()

	if !ok {
		return
	}

	_ = stream.closeWith(false, nil)
}

// teardown closes every remaining stream, records why the loop exited, and
// signals Done. Streams torn down by an error surface that error to their
// readers; on a clean shutdown they surface ErrTunnelSessionClosed, because
// the tunnel ended without an orderly per-stream FrameClose.
func (s *TunnelSession) teardown(loopErr error) {
	s.stateMu.Lock()
	s.closed = true
	s.loopErr = loopErr
	s.stateMu.Unlock()

	readErr := loopErr
	if readErr == nil {
		readErr = ErrTunnelSessionClosed
	}

	for _, stream := range s.snapshotStreams() {
		_ = stream.closeWith(false, readErr)
	}

	close(s.done)
}

// snapshotStreams copies the live stream set out from under the lock so
// callers can close streams without holding it.
func (s *TunnelSession) snapshotStreams() []*TunnelStream {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	streams := make([]*TunnelStream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}

	return streams
}

// unregister forgets a stream so later frames for its StreamID are treated as
// having raced the close.
func (s *TunnelSession) unregister(streamID uint32) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	delete(s.streams, streamID)
}

// isClosing reports whether Close has been requested, so the demux loop can
// tell a deliberate shutdown from a transport failure.
func (s *TunnelSession) isClosing() bool {
	select {
	case <-s.closing:
		return true
	default:
		return false
	}
}

// writeFrame serializes frame writes from all streams onto the shared
// channel, keeping each frame's header and payload contiguous.
func (s *TunnelSession) writeFrame(frame Frame) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return WriteFrame(s.writer, frame)
}

// closeIfCloser closes a channel half when it supports it; a bare io.Reader/
// io.Writer is left alone (Close then relies on the peer ending the stream).
func closeIfCloser(half any) {
	closer, ok := half.(io.Closer)
	if ok {
		_ = closer.Close()
	}
}

// TunnelStream is one multiplexed connection on a [TunnelSession]: an
// io.ReadWriteCloser whose bytes travel as [FrameData] frames tagged with its
// StreamID. There is no half-close: [TunnelStream.Close] (or the peer's)
// ends the stream in both directions, matching the codec's FrameClose
// semantics.
type TunnelStream struct {
	id      uint32
	session *TunnelSession

	inbound *inboundBuffer

	closeOnce sync.Once
	closed    chan struct{}
}

// newTunnelStream wires a stream to its session with the buffer the demux
// loop feeds inbound data through.
func newTunnelStream(session *TunnelSession, streamID uint32) *TunnelStream {
	return &TunnelStream{
		id:        streamID,
		session:   session,
		inbound:   newInboundBuffer(),
		closeOnce: sync.Once{},
		closed:    make(chan struct{}),
	}
}

// StreamID identifies this stream on the tunnel.
func (s *TunnelStream) StreamID() uint32 {
	return s.id
}

// Read returns bytes the peer sent on this stream. Once buffered bytes are
// drained it reports io.EOF after the peer closed the stream,
// ErrTunnelStreamClosed after a local close, and the session's failure
// reason if the whole tunnel tore down.
func (s *TunnelStream) Read(data []byte) (int, error) {
	return s.inbound.Read(data)
}

// Write sends bytes to the peer on this stream, transparently splitting them
// into frames of at most MaxTunnelPayload.
func (s *TunnelStream) Write(data []byte) (int, error) {
	select {
	case <-s.closed:
		return 0, ErrTunnelStreamClosed
	default:
	}

	written := 0
	for written < len(data) {
		chunk := min(len(data)-written, MaxTunnelPayload)

		err := s.session.writeFrame(Frame{
			StreamID: s.id,
			Type:     FrameData,
			Payload:  data[written : written+chunk],
		})
		if err != nil {
			return written, err
		}

		written += chunk
	}

	return written, nil
}

// Close ends the stream in both directions: the peer is told via FrameClose
// and local reads unblock with ErrTunnelStreamClosed. Closing again is a
// no-op.
func (s *TunnelStream) Close() error {
	return s.closeWith(true, ErrTunnelStreamClosed)
}

// closeWith is the single teardown path for a stream: it unregisters the
// stream, optionally notifies the peer (local closes only), and ends the
// inbound buffer with readErr (nil surfaces io.EOF once drained — the
// peer-close case).
func (s *TunnelStream) closeWith(sendClose bool, readErr error) error {
	var frameErr error

	s.closeOnce.Do(func() {
		close(s.closed)
		s.session.unregister(s.id)

		if sendClose {
			frameErr = s.session.writeFrame(Frame{StreamID: s.id, Type: FrameClose, Payload: nil})
		}

		s.inbound.close(readErr)
	})

	return frameErr
}

// inboundBuffer is the bounded, demux-fed receive side of one stream. It
// exists so the shared demux loop only blocks when a stream is left unread
// past tunnelReceiveBudget — an unbuffered handoff would deadlock ordinary
// sequential write-then-close peers against the single shared channel.
type inboundBuffer struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	notFull  *sync.Cond
	data     []byte
	closed   bool
	readErr  error
}

// newInboundBuffer returns an empty, open buffer.
func newInboundBuffer() *inboundBuffer {
	buffer := &inboundBuffer{
		mu:       sync.Mutex{},
		notEmpty: nil,
		notFull:  nil,
		data:     nil,
		closed:   false,
		readErr:  nil,
	}
	buffer.notEmpty = sync.NewCond(&buffer.mu)
	buffer.notFull = sync.NewCond(&buffer.mu)

	return buffer
}

// Read hands out buffered bytes, blocking while the buffer is open and
// empty. Once the buffer is closed and drained it reports readErr, or io.EOF
// when the stream ended cleanly.
func (b *inboundBuffer) Read(out []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for len(b.data) == 0 && !b.closed {
		b.notEmpty.Wait()
	}

	if len(b.data) == 0 {
		if b.readErr != nil {
			return 0, b.readErr
		}

		return 0, io.EOF
	}

	count := copy(out, b.data)
	b.data = b.data[count:]
	b.notFull.Signal()

	return count, nil
}

// write appends an inbound chunk, blocking while the budget is exhausted —
// the deliberate per-stream backpressure on the demux loop. Chunks arriving
// after close raced a teardown and are dropped.
func (b *inboundBuffer) write(chunk []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for !b.closed && len(b.data)+len(chunk) > tunnelReceiveBudget {
		b.notFull.Wait()
	}

	if b.closed {
		return
	}

	b.data = append(b.data, chunk...)
	b.notEmpty.Signal()
}

// close ends the buffer: blocked readers and the demux wake up, later chunks
// are dropped, and reads surface readErr (io.EOF when nil) once the
// remaining bytes drain.
func (b *inboundBuffer) close(readErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	b.closed = true
	b.readErr = readErr
	b.notEmpty.Broadcast()
	b.notFull.Broadcast()
}
