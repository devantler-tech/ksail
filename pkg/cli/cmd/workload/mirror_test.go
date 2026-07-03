package workload_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

func TestMirrorCmdRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	for _, port := range []string{"0", "-1", "65536"} {
		cmd := workload.NewMirrorCmd()
		cmd.SetArgs([]string{mirrorTestDeploy, "--port", port})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)

		err := cmd.Execute()
		require.ErrorIs(t, err, workload.ErrInvalidMirrorPort, "port %s must be rejected", port)
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
