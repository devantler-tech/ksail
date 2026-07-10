package workload_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

const (
	mirrorTestNamespace = "default"
	mirrorTestDeploy    = "my-app"
	mirrorTestPod       = "my-app-1"
	mirrorTestContainer = "app"
)

// errMirrorReplayDialRefused is the static dial failure the --to tests inject.
var errMirrorReplayDialRefused = errors.New("connection refused")

// errMirrorInterruptIgnored reports that raising the documented interrupt did
// not cancel the capture context.
var errMirrorInterruptIgnored = errors.New("interrupt did not cancel the capture context")

// mirrorSelectorLabels is the label set the test Deployment selects on.
func mirrorSelectorLabels() map[string]string {
	return map[string]string{"app": mirrorTestDeploy}
}

// newMirrorDeployment builds a single-container Deployment for the mirror tests.
func newMirrorDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: mirrorTestDeploy, Namespace: mirrorTestNamespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: mirrorSelectorLabels()},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: mirrorTestContainer}},
				},
			},
		},
	}
}

// newMirrorPod builds a Running pod matching the test Deployment whose tap
// container status is already Running, so WaitForTap succeeds against the
// fake clientset (no kubelet updates statuses there). withTapSpec also puts
// the tap into the pod spec, making InjectTap take its reuse path.
func newMirrorPod(withTapSpec bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mirrorTestPod,
			Namespace: mirrorTestNamespace,
			Labels:    mirrorSelectorLabels(),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: mirrorTestContainer}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			EphemeralContainerStatuses: []corev1.ContainerStatus{
				{
					Name: mirror.TapContainerName,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
			},
		},
	}

	if withTapSpec {
		pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{
			{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name: mirror.TapContainerName,
				},
				TargetContainerName: mirrorTestContainer,
			},
		}
	}

	return pod
}

// testPcapBytes returns a minimal valid pcap stream with one packet.
func testPcapBytes(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer

	writer := pcapgo.NewWriter(&buf)
	require.NoError(t, writer.WriteFileHeader(65536, layers.LinkTypeLinuxSLL2))

	payload := []byte{0x01, 0x02, 0x03, 0x04}
	require.NoError(t, writer.WritePacket(gopacketCaptureInfo(len(payload)), payload))

	return buf.Bytes()
}

// stubMirrorSession installs mirror-command seams that serve the given fake
// clientset and write pcap into the capture writer, and returns a restore
// function plus a pointer flag recording whether the session ran.
func stubMirrorSession(
	client kubernetes.Interface,
	pcap []byte,
) (func(), *bool) {
	ran := new(bool)

	restoreClients := workload.ExportSetMirrorClients(
		func(_, _ string) (kubernetes.Interface, *rest.Config, error) {
			return client, &rest.Config{}, nil
		},
	)

	restoreSession := workload.ExportSetRunCaptureSession(
		func(
			_ context.Context,
			_ kubernetes.Interface,
			_ *rest.Config,
			_ *mirror.TapPoint,
			_ int,
			out io.Writer,
			_ ...mirror.CaptureSessionOption,
		) error {
			*ran = true

			_, err := out.Write(pcap)
			if err != nil {
				return fmt.Errorf("write pcap to capture output: %w", err)
			}

			return nil
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}, ran
}

func TestMirrorCmdRequiresPort(t *testing.T) {
	t.Parallel()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestMirrorCmdHelpDocumentsCleanInterruptExit(t *testing.T) {
	t.Parallel()

	cmd := workload.NewMirrorCmd()

	assert.Contains(t, cmd.Long, "Ctrl-C stops it cleanly with exit status 0")
}

func TestMirrorCmdRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	for _, port := range []string{"0", "-1", "65536"} {
		t.Run(port, func(t *testing.T) {
			t.Parallel()

			cmd := workload.NewMirrorCmd()
			cmd.SetArgs([]string{mirrorTestDeploy, "--port", port})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			require.ErrorIs(t, err, workload.ErrInvalidMirrorPort, "port %s must be rejected", port)
		})
	}
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdCreatesNestedOutputDirectories(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restore, _ := stubMirrorSession(client, testPcapBytes(t))
	defer restore()

	nested := filepath.Join("captures", "nested", "out.pcap")

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080", "--output", nested})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	_, err := os.Stat(nested)
	require.NoError(t, err, "nested output directories must be created")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdCapturesToFileAndSummarizes(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restore, sessionRan := stubMirrorSession(client, testPcapBytes(t))
	defer restore()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	assert.True(t, *sessionRan, "the capture session must run")
	assert.Contains(t, out.String(), "injected read-only tap")
	assert.Contains(t, out.String(), "captured 1 packets (4 bytes)")

	_, err := os.Stat(filepath.Join(".", "mirror.pcap"))
	require.NoError(t, err, "the default capture file must exist")
}

// stubInterruptingMirrorSession installs mirror-command seams whose capture
// session writes the given pcap, raises the documented interrupt at the test
// process, and then blocks until the interrupt cancels the session context —
// without the command's signal-aware context, the default disposition kills
// the test process at the raise.
func stubInterruptingMirrorSession(client kubernetes.Interface, pcap []byte) func() {
	restoreClients := workload.ExportSetMirrorClients(
		func(_, _ string) (kubernetes.Interface, *rest.Config, error) {
			return client, &rest.Config{}, nil
		},
	)

	restoreSession := workload.ExportSetRunCaptureSession(
		func(
			ctx context.Context,
			_ kubernetes.Interface,
			_ *rest.Config,
			_ *mirror.TapPoint,
			_ int,
			out io.Writer,
			_ ...mirror.CaptureSessionOption,
		) error {
			_, err := out.Write(pcap)
			if err != nil {
				return fmt.Errorf("write pcap to capture output: %w", err)
			}

			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				return fmt.Errorf("find own process: %w", err)
			}

			err = proc.Signal(os.Interrupt)
			if err != nil {
				return fmt.Errorf("raise interrupt: %w", err)
			}

			// Block like the real exec stream until the interrupt cancels the
			// context, then report the clean stop the service layer maps a
			// cancelled capture to.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
				return errMirrorInterruptIgnored
			}
		},
	)

	return func() {
		restoreSession()
		restoreClients()
	}
}

// skipIfInterruptUnsupported skips interrupt-raising tests on Windows, where
// os.Interrupt cannot be sent to the running process.
func skipIfInterruptUnsupported(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("raising os.Interrupt at the running process is not supported on Windows")
	}
}

// TestMirrorCmdInterruptStopsCaptureAndSummarizes verifies that an interrupted
// capture still closes cleanly and reports the packets written before shutdown.
//
//nolint:paralleltest // t.Chdir is incompatible with t.Parallel; the raised SIGINT is process-wide.
func TestMirrorCmdInterruptStopsCaptureAndSummarizes(t *testing.T) {
	skipIfInterruptUnsupported(t)

	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restore := stubInterruptingMirrorSession(client, testPcapBytes(t))
	defer restore()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute(), "an interrupted capture must end as a clean stop")
	assert.Contains(t, out.String(), "captured 1 packets (4 bytes)",
		"the capture summary must still print after Ctrl-C")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdReusesExistingTap(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(true))

	restore, _ := stubMirrorSession(client, testPcapBytes(t))
	defer restore()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "reusing the tap already injected")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdStreamsToStdout(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	pcap := testPcapBytes(t)

	restore, _ := stubMirrorSession(client, pcap)
	defer restore()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080", "--output", "-"})

	var out, errOut bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), string(pcap), "stdout must carry the raw pcap stream")
	assert.NotContains(t, out.String(), "captured 1 packets", "stdout mode skips the summary")

	_, err := os.Stat(filepath.Join(".", "mirror.pcap"))
	require.ErrorIs(t, err, os.ErrNotExist, "stdout mode must not create a capture file")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdFailsForMissingDeployment(t *testing.T) {
	t.Chdir(t.TempDir())

	restore, _ := stubMirrorSession(k8sfake.NewClientset(), nil)
	defer restore()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, mirror.ErrDeploymentNotFound)
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdFailsForCorruptCapture(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restore, _ := stubMirrorSession(client, []byte("not a pcap stream"))
	defer restore()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarize capture")
}

// gopacketCaptureInfo builds the capture metadata for a single test packet.
func gopacketCaptureInfo(length int) gopacket.CaptureInfo {
	return gopacket.CaptureInfo{
		Timestamp:     time.Unix(0, 0),
		CaptureLength: length,
		Length:        length,
	}
}

// replayTestPcap builds a one-flow LINUX_SLL2 pcap stream whose inbound
// payload is "ping" on port 8080, for the --to replay tests.
func replayTestPcap(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer

	writer := pcapgo.NewWriter(&buf)
	require.NoError(t, writer.WriteFileHeader(262144, layers.LinkTypeLinuxSLL2))

	ipLayer := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    net.IPv4(10, 0, 0, 1),
		DstIP:    net.IPv4(10, 0, 0, 2),
	}
	tcpLayer := &layers.TCP{
		SrcPort: 40000,
		DstPort: 8080,
		Seq:     1,
		Window:  65535,
	}
	require.NoError(t, tcpLayer.SetNetworkLayerForChecksum(ipLayer))

	serialized := gopacket.NewSerializeBuffer()
	require.NoError(t, gopacket.SerializeLayers(
		serialized,
		gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true},
		ipLayer, tcpLayer, gopacket.Payload([]byte("ping")),
	))

	packet := make([]byte, 20+len(serialized.Bytes()))
	binary.BigEndian.PutUint16(packet[0:2], 0x0800) // EtherType: IPv4
	binary.BigEndian.PutUint16(packet[8:10], 1)     // ARPHRD_ETHER
	packet[11] = 6
	copy(packet[20:], serialized.Bytes())

	require.NoError(t, writer.WritePacket(gopacketCaptureInfo(len(packet)), packet))

	return buf.Bytes()
}

func TestMirrorCmdRejectsInvalidReplayTarget(t *testing.T) {
	t.Parallel()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080", "--to", "not-an-address"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.ErrorIs(t, err, workload.ErrInvalidMirrorReplayTarget)
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdReplaysToLocalAddress(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restore, _ := stubMirrorSession(client, replayTestPcap(t))
	defer restore()

	received := make(chan string, 1)

	restoreReplay := workload.ExportSetNewLiveReplay(
		func(address string, port int) (*mirror.LiveReplay, error) {
			return mirror.NewLiveReplay(address, port, mirror.WithReplayDialer(
				func(_, _ string) (net.Conn, error) {
					clientConn, serverConn := net.Pipe()

					go func() {
						data, _ := io.ReadAll(serverConn)
						received <- string(data)
					}()

					return clientConn, nil
				},
			))
		},
	)
	defer restoreReplay()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080", "--to", "localhost:9999"})

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "replaying mirrored inbound traffic to localhost:9999")
	assert.Equal(t, "ping", <-received, "the local connection must receive the inbound payload")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdReplaysWhileStreamingToStdout(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	pcap := replayTestPcap(t)

	restore, _ := stubMirrorSession(client, pcap)
	defer restore()

	received := make(chan string, 1)

	restoreReplay := workload.ExportSetNewLiveReplay(
		func(address string, port int) (*mirror.LiveReplay, error) {
			return mirror.NewLiveReplay(address, port, mirror.WithReplayDialer(
				func(_, _ string) (net.Conn, error) {
					clientConn, serverConn := net.Pipe()

					go func() {
						data, _ := io.ReadAll(serverConn)
						received <- string(data)
					}()

					return clientConn, nil
				},
			))
		},
	)
	defer restoreReplay()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{
		mirrorTestDeploy, "--port", "8080", "--output", "-", "--to", "localhost:9999",
	})

	var out, errOut bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	require.NoError(t, cmd.Execute())

	// The tee must feed BOTH sinks: the raw pcap stream to stdout (the
	// tshark-piping path) and the inbound payload to the replay connection.
	assert.Contains(t, out.String(), string(pcap), "stdout must carry the raw pcap stream")

	select {
	case data := <-received:
		assert.Equal(t, "ping", data, "the local connection must receive the inbound payload")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the replay connection to deliver the inbound payload")
	}

	_, err := os.Stat(filepath.Join(".", "mirror.pcap"))
	require.ErrorIs(t, err, os.ErrNotExist, "stdout mode must not create a capture file")
}

//nolint:paralleltest // t.Chdir is incompatible with t.Parallel.
func TestMirrorCmdSurfacesReplayDialFailure(t *testing.T) {
	t.Chdir(t.TempDir())

	client := k8sfake.NewClientset(newMirrorDeployment(), newMirrorPod(false))

	restore, _ := stubMirrorSession(client, replayTestPcap(t))
	defer restore()

	restoreReplay := workload.ExportSetNewLiveReplay(
		func(address string, port int) (*mirror.LiveReplay, error) {
			return mirror.NewLiveReplay(address, port, mirror.WithReplayDialer(
				func(_, _ string) (net.Conn, error) {
					return nil, errMirrorReplayDialRefused
				},
			))
		},
	)
	defer restoreReplay()

	cmd := workload.NewMirrorCmd()
	cmd.SetArgs([]string{mirrorTestDeploy, "--port", "8080", "--to", "localhost:9999"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live replay")
	assert.Contains(t, err.Error(), "connection refused")
}
