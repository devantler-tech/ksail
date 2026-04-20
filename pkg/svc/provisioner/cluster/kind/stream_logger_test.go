package kindprovisioner_test

import (
	"bytes"
	"testing"

	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/log"
)

// --- streamLogger ---

func TestStreamLogger_Info(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger, ok := logger.(log.InfoLogger)
	require.True(t, ok)
	infoLogger.Info("hello world")

	assert.Equal(t, "hello world\n", buf.String())
}

func TestStreamLogger_Infof(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger, ok := logger.(log.InfoLogger)
	require.True(t, ok)
	infoLogger.Infof("count: %d", 42)

	assert.Equal(t, "count: 42\n", buf.String())
}

func TestStreamLogger_Warn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	logger.Warn("caution")

	assert.Equal(t, "caution\n", buf.String())
}

func TestStreamLogger_Warnf(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	logger.Warnf("warning %s", "msg")

	assert.Equal(t, "warning msg\n", buf.String())
}

func TestStreamLogger_Error(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	logger.Error("failure")

	assert.Equal(t, "failure\n", buf.String())
}

func TestStreamLogger_Errorf(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	logger.Errorf("error: %v", "details")

	assert.Equal(t, "error: details\n", buf.String())
}

func TestStreamLogger_V0_ReturnsEnabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger := logger.V(0)
	assert.True(t, infoLogger.Enabled())
}

func TestStreamLogger_V0_WritesToBuffer(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger := logger.V(0)
	infoLogger.Info("v0 message")

	assert.Equal(t, "v0 message\n", buf.String())
}

func TestStreamLogger_V1_ReturnsDisabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger := logger.V(1)
	assert.False(t, infoLogger.Enabled())
}

func TestStreamLogger_V1_NoOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger := logger.V(1)
	infoLogger.Info("should be suppressed")

	assert.Empty(t, buf.String(), "V(1) should suppress info messages")
}

func TestStreamLogger_V2_NoOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	infoLogger := logger.V(2)
	infoLogger.Info("also suppressed")

	assert.Empty(t, buf.String(), "V(2) should suppress info messages")
}

func TestStreamLogger_Write_EmptyMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	// An empty message should still produce a newline
	infoLogger, ok := logger.(log.InfoLogger)
	require.True(t, ok)
	infoLogger.Info("")

	assert.Equal(t, "\n", buf.String())
}

func TestStreamLogger_Write_MessageWithNewline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	// Message already ending with \n should not get another one
	infoLogger, ok := logger.(log.InfoLogger)
	require.True(t, ok)
	infoLogger.Info("already terminated\n")

	assert.Equal(t, "already terminated\n", buf.String())
}

func TestStreamLogger_Write_MessageWithCarriageReturn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	// Messages with carriage return are passed through as-is
	infoLogger, ok := logger.(log.InfoLogger)
	require.True(t, ok)
	infoLogger.Info("progress\r")

	assert.Equal(t, "progress\r", buf.String())
}

func TestStreamLogger_Enabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := kindprovisioner.NewStreamLoggerForTest(&buf)

	// The streamLogger itself should be enabled (it implements InfoLogger for V(0))
	infoLogger, ok := logger.(log.InfoLogger)
	require.True(t, ok)
	assert.True(t, infoLogger.Enabled())
}

// --- setName ---

func TestSetName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		inputName      string
		kindConfigName string
		want           string
	}{
		{
			name:           "uses input name when provided",
			inputName:      "my-cluster",
			kindConfigName: "config-default",
			want:           "my-cluster",
		},
		{
			name:           "falls back to config name when empty",
			inputName:      "",
			kindConfigName: "config-default",
			want:           "config-default",
		},
		{
			name:           "both empty returns empty",
			inputName:      "",
			kindConfigName: "",
			want:           "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := kindprovisioner.SetNameForTest(tc.inputName, tc.kindConfigName)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- NewProvisioner ---

func TestNewProvisioner_SetsKubeConfig(t *testing.T) {
	t.Parallel()

	provisioner, _, _ := newProvisionerForTest(t)
	require.Equal(t, "~/.kube/config", provisioner.KubeConfigForTest())
}
