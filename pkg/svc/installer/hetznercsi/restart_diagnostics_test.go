package hetznercsiinstaller_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	hetznercsiinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/hetznercsi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errCSIChartUnready = errors.New("simulated CSI chart readiness failure")

func TestInstallRecordsControllerTerminationBeforeHelmReturns(t *testing.T) {
	t.Setenv("HCLOUD_TOKEN", "test-token")

	restore := hetznercsiinstaller.SetWaitForCCMNodeLabelsFnForTest(
		func(context.Context, string, string, time.Duration) error { return nil },
	)
	t.Cleanup(restore)

	createdAt := time.Now().UTC().Truncate(time.Second).Add(time.Second)
	server := httptest.NewServer(csiRestartAPI(t, createdAt))
	t.Cleanup(server.Close)

	logs := make(restartLogs, 1)
	previousLogger := slog.Default()

	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	helmClient := helm.NewMockInterface(t)
	helmClient.EXPECT().GetReleaseStorageLabels(mock.Anything, "hcloud-csi", "kube-system").
		Return(nil, nil)
	helmClient.EXPECT().AddRepository(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	var observed string

	helmClient.EXPECT().InstallOrUpgradeChart(mock.Anything, mock.Anything).
		Run(func(context.Context, *helm.ChartSpec) {
			timer := time.NewTimer(time.Second)
			defer timer.Stop()

			select {
			case observed = <-logs:
			case <-timer.C:
			}
		}).
		Return(nil, errCSIChartUnready).
		Once()

	installer := hetznercsiinstaller.NewInstaller(
		helmClient, csiRestartKubeconfig(t, server.URL), "test", time.Minute, "", true,
	)
	err := installer.Install(t.Context())

	require.ErrorIs(t, err, errCSIChartUnready)
	require.NotEmpty(t, observed, "capture a driver termination while Helm waits")

	finishedAt := createdAt.Add(18 * time.Second).Format(time.RFC3339)

	assert.Contains(t, observed, `"pod":"hcloud-csi-controller-test"`)
	assert.Contains(t, observed, `"restart_count":1`)
	assert.Contains(t, observed, `"exit_code":143`)
	assert.Contains(t, observed, `"signal":15`)
	assert.Contains(t, observed, `"finished_at":"`+finishedAt+`"`)
	assert.NotContains(t, observed, "do-not-log")
}

type restartLogs chan string

func (logs restartLogs) Write(message []byte) (int, error) {
	if strings.Contains(string(message), "observed Hetzner CSI controller driver termination") {
		select {
		case logs <- string(message):
		default:
		}
	}

	return len(message), nil
}

func csiRestartAPI(t *testing.T, createdAt time.Time) http.Handler {
	t.Helper()

	started := createdAt.Format(time.RFC3339)
	finished := createdAt.Add(18 * time.Second).Format(time.RFC3339)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")

		var err error

		switch request.URL.Path {
		case "/api/v1/namespaces/kube-system/secrets/hcloud":
			_, err = fmt.Fprint(response, `{"apiVersion":"v1","kind":"Secret",
				"metadata":{"name":"hcloud","namespace":"kube-system"},"data":{"token":"dGVzdC10b2tlbg=="}}`)
		case "/api/v1/namespaces/kube-system/pods":
			_, err = fmt.Fprintf(response, `{"apiVersion":"v1","kind":"PodList","items":[{
				"metadata":{"name":"hcloud-csi-controller-test","namespace":"kube-system",
					"uid":"csi-test","creationTimestamp":%q,"labels":{"app.kubernetes.io/instance":"hcloud-csi"}},
				"status":{"containerStatuses":[
					{"name":"csi-attacher","restartCount":4,"lastState":{"terminated":{"exitCode":99}}},
					{"name":"hcloud-csi-driver","restartCount":1,"lastState":{"terminated":{
						"exitCode":143,"signal":15,"reason":"Error","message":"do-not-log",
						"startedAt":%q,"finishedAt":%q}}}
				]}
			}]}`, started, started, finished)
		default:
			http.NotFound(response, request)
		}

		if err != nil {
			t.Errorf("write Kubernetes fixture response: %v", err)
		}
	})
}

func csiRestartKubeconfig(t *testing.T, endpoint string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	config := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
users:
- name: test
  user: {}
current-context: test
`, endpoint)
	require.NoError(t, os.WriteFile(path, []byte(config), 0o600))

	return path
}
