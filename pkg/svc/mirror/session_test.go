package mirror_test

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var errExecFailed = errors.New("exec failed")

// stubExecutor implements mirror.CaptureExecutor: it writes canned bytes to
// the stream's stdout/stderr and returns a canned error.
type stubExecutor struct {
	stdout []byte
	stderr string
	err    error
}

func (s *stubExecutor) StreamWithContext(
	_ context.Context,
	options remotecommand.StreamOptions,
) error {
	if len(s.stdout) > 0 && options.Stdout != nil {
		_, _ = options.Stdout.Write(s.stdout)
	}

	if s.stderr != "" && options.Stderr != nil {
		_, _ = options.Stderr.Write([]byte(s.stderr))
	}

	return s.err
}

func stubFactory(executor mirror.CaptureExecutor, execURL **url.URL) mirror.CaptureExecutorFactory {
	return func(_ *rest.Config, _ string, requestURL *url.URL) (mirror.CaptureExecutor, error) {
		if execURL != nil {
			*execURL = requestURL
		}

		return executor, nil
	}
}

func newSessionClient(t *testing.T) kubernetes.Interface {
	t.Helper()

	client, err := kubernetes.NewForConfig(&rest.Config{Host: "https://127.0.0.1:6443"})
	require.NoError(t, err)

	return client
}

// pcapStream builds a valid pcap byte stream carrying the given payloads.
func pcapStream(t *testing.T, payloads ...[]byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	writer := pcapgo.NewWriter(&buf)
	require.NoError(t, writer.WriteFileHeader(65536, layers.LinkTypeEthernet))

	for _, payload := range payloads {
		info := gopacket.CaptureInfo{
			Timestamp:      time.Unix(0, 0),
			CaptureLength:  len(payload),
			Length:         len(payload),
			InterfaceIndex: 0,
			AncillaryData:  nil,
		}
		require.NoError(t, writer.WritePacket(info, payload))
	}

	return buf.Bytes()
}

func TestRunCaptureSession_StreamsPcapToWriter(t *testing.T) {
	t.Parallel()

	stream := pcapStream(t, []byte("first"), []byte("second"))
	executor := &stubExecutor{stdout: stream, stderr: "", err: nil}

	var out bytes.Buffer

	err := mirror.RunCaptureSession(
		t.Context(), newSessionClient(t), &rest.Config{}, newTapPoint(), 8080, &out,
		mirror.WithCaptureExecutorFactory(stubFactory(executor, nil)),
	)

	require.NoError(t, err)
	assert.Equal(t, stream, out.Bytes())

	summary, err := mirror.SummarizeCapture(&out)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.Packets)
	assert.Equal(t, len("first")+len("second"), summary.Bytes)
	assert.Equal(t, layers.LinkTypeEthernet, summary.LinkType)
}

func TestRunCaptureSession_TargetsTapContainerWithCaptureCommand(t *testing.T) {
	t.Parallel()

	var execURL *url.URL

	executor := &stubExecutor{stdout: nil, stderr: "", err: nil}
	point := newTapPoint()

	err := mirror.RunCaptureSession(
		t.Context(), newSessionClient(t), &rest.Config{}, point, 9090, &bytes.Buffer{},
		mirror.WithCaptureExecutorFactory(stubFactory(executor, &execURL)),
	)

	require.NoError(t, err)
	require.NotNil(t, execURL)
	assert.Contains(t, execURL.Path,
		"/namespaces/"+point.Namespace+"/pods/"+point.Pod+"/exec")

	query := execURL.Query()
	assert.Equal(t, mirror.TapContainerName, query.Get("container"))

	wantCommand, err := mirror.CaptureCommand(9090)
	require.NoError(t, err)
	assert.Equal(t, wantCommand, query["command"])
}

func TestRunCaptureSession_SurfacesRemoteStderrOnFailure(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{
		stdout: nil,
		stderr: "tcpdump: pcap_loop: The interface disappeared\n",
		err:    errExecFailed,
	}

	err := mirror.RunCaptureSession(
		t.Context(), newSessionClient(t), &rest.Config{}, newTapPoint(), 8080, &bytes.Buffer{},
		mirror.WithCaptureExecutorFactory(stubFactory(executor, nil)),
	)

	require.ErrorIs(t, err, errExecFailed)
	assert.Contains(t, err.Error(), "The interface disappeared")
}

func TestRunCaptureSession_CancelledContextIsCleanStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	executor := &stubExecutor{stdout: nil, stderr: "", err: context.Canceled}

	err := mirror.RunCaptureSession(
		ctx, newSessionClient(t), &rest.Config{}, newTapPoint(), 8080, &bytes.Buffer{},
		mirror.WithCaptureExecutorFactory(stubFactory(executor, nil)),
	)

	require.NoError(t, err)
}

func TestRunCaptureSession_ExecFailureWithDoneContextIsSurfaced(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	executor := &stubExecutor{
		stdout: nil,
		stderr: "tcpdump: permission denied\n",
		err:    errExecFailed,
	}

	err := mirror.RunCaptureSession(
		ctx, newSessionClient(t), &rest.Config{}, newTapPoint(), 8080, &bytes.Buffer{},
		mirror.WithCaptureExecutorFactory(stubFactory(executor, nil)),
	)

	require.ErrorIs(t, err, errExecFailed)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestRunCaptureSession_DeadlineExceededIsCleanStop(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{stdout: nil, stderr: "", err: context.DeadlineExceeded}

	err := mirror.RunCaptureSession(
		t.Context(), newSessionClient(t), &rest.Config{}, newTapPoint(), 8080, &bytes.Buffer{},
		mirror.WithCaptureExecutorFactory(stubFactory(executor, nil)),
	)

	require.NoError(t, err)
}

func TestRunCaptureSession_Guards(t *testing.T) {
	t.Parallel()

	client := newSessionClient(t)

	err := mirror.RunCaptureSession(
		t.Context(), client, &rest.Config{}, nil, 8080, &bytes.Buffer{},
	)
	require.ErrorIs(t, err, mirror.ErrTapPointNil)

	err = mirror.RunCaptureSession(
		t.Context(), nil, &rest.Config{}, newTapPoint(), 8080, &bytes.Buffer{},
	)
	require.ErrorIs(t, err, mirror.ErrCaptureClientNil)

	err = mirror.RunCaptureSession(
		t.Context(), client, nil, newTapPoint(), 8080, &bytes.Buffer{},
	)
	require.ErrorIs(t, err, mirror.ErrCaptureRESTConfigNil)

	err = mirror.RunCaptureSession(
		t.Context(), client, &rest.Config{}, newTapPoint(), 8080, nil,
	)
	require.ErrorIs(t, err, mirror.ErrCaptureWriterNil)

	err = mirror.RunCaptureSession(
		t.Context(), client, &rest.Config{}, newTapPoint(), 0, &bytes.Buffer{},
	)
	require.ErrorIs(t, err, mirror.ErrInvalidCapturePort)
}

func TestSummarizeCapture_RejectsGarbage(t *testing.T) {
	t.Parallel()

	_, err := mirror.SummarizeCapture(strings.NewReader("not a pcap stream"))

	require.Error(t, err)
}

func TestSummarizeCapture_RejectsTruncatedStream(t *testing.T) {
	t.Parallel()

	stream := pcapStream(t, []byte("payload"))
	truncated := stream[:len(stream)-3]

	_, err := mirror.SummarizeCapture(bytes.NewReader(truncated))

	require.Error(t, err)
}
