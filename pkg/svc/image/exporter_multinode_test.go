package image_test

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type multiNodeExportCase struct {
	distribution v1alpha1.Distribution
	nodes        []container.Summary
	// imageLists maps each node that is probed to its `ctr images list -q` output.
	imageLists     map[string]string
	exportNode     string
	tmpPath        string
	requested      []string
	expectedExport []string
}

func kindMultiNodeSummaries() []container.Summary {
	return []container.Summary{
		{
			Names:  []string{"/my-cluster-control-plane"},
			Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
		},
		{
			Names:  []string{"/my-cluster-worker"},
			Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
		},
		{
			Names:  []string{"/my-cluster-worker2"},
			Labels: map[string]string{"io.x-k8s.kind.role": "worker"},
		},
	}
}

func runMultiNodeExportTest(t *testing.T, testCase multiNodeExportCase) {
	t.Helper()

	ctx := context.Background()
	mockClient := docker.NewMockAPIClient(t)
	outputPath := filepath.Join(t.TempDir(), "images.tar")

	mockClient.EXPECT().ContainerList(ctx, mock.Anything).Return(testCase.nodes, nil)

	// Each probed node is listed exactly once: the chosen node's listing is
	// reused to resolve the requested refs, never read a second time.
	for nodeName, imageList := range testCase.imageLists {
		setupExecMockWithStdoutForExporter(
			ctx, t, mockClient, nodeName,
			[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
			imageList,
		)
	}

	setupPlatformDetectMockForExporter(ctx, t, mockClient, testCase.exportNode)

	exportCmd := append([]string{
		ctrCommand, "--namespace=k8s.io", "images", "export",
		"--platform", "linux/amd64", "--local", testCase.tmpPath,
	}, testCase.expectedExport...)
	setupExecMockWithCmdForExporter(ctx, t, mockClient, testCase.exportNode, exportCmd)

	tarContent := createExportTar(t, []byte("fake image data"))
	mockClient.EXPECT().
		CopyFromContainer(ctx, testCase.exportNode, testCase.tmpPath).
		Return(io.NopCloser(bytes.NewReader(tarContent)), container.PathStat{}, nil)

	setupExecMockForExporter(ctx, t, mockClient, testCase.exportNode)

	err := image.NewExporter(mockClient).Export(
		ctx, "my-cluster", testCase.distribution, v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath, Images: testCase.requested},
	)

	require.NoError(t, err)
}

// An image pulled only by a worker's kubelet is exported from that worker.
func TestExportUsesWorkerHoldingRequestedImage(t *testing.T) {
	t.Parallel()

	runMultiNodeExportTest(t, multiNodeExportCase{
		distribution: v1alpha1.DistributionVanilla,
		nodes:        kindMultiNodeSummaries(),
		imageLists: map[string]string{
			"my-cluster-control-plane": "registry.k8s.io/pause:3.10\n",
			"my-cluster-worker":        "registry.k8s.io/pause:3.10\n",
			"my-cluster-worker2": "registry.k8s.io/pause:3.10\n" +
				"docker.io/traefik/whoami:v1.10@sha256:abc123\n",
		},
		exportNode:     "my-cluster-worker2",
		tmpPath:        kindExporterTarPath,
		requested:      []string{"traefik/whoami:v1.10"},
		expectedExport: []string{"docker.io/traefik/whoami:v1.10@sha256:abc123"},
	})
}

// When the preferred node already holds every requested image, no other node is asked.
func TestExportStopsProbingAtFirstNodeHoldingAllImages(t *testing.T) {
	t.Parallel()

	runMultiNodeExportTest(t, multiNodeExportCase{
		distribution: v1alpha1.DistributionVanilla,
		nodes:        kindMultiNodeSummaries(),
		imageLists: map[string]string{
			"my-cluster-control-plane": "docker.io/library/nginx:latest\n",
		},
		exportNode:     "my-cluster-control-plane",
		tmpPath:        kindExporterTarPath,
		requested:      []string{"nginx:latest"},
		expectedExport: []string{"docker.io/library/nginx:latest"},
	})
}

// With images split across nodes, the node holding the most requested images wins.
func TestExportPrefersNodeHoldingMostRequestedImages(t *testing.T) {
	t.Parallel()

	runMultiNodeExportTest(t, multiNodeExportCase{
		distribution: v1alpha1.DistributionVanilla,
		nodes:        kindMultiNodeSummaries(),
		imageLists: map[string]string{
			"my-cluster-control-plane": "",
			"my-cluster-worker":        "docker.io/library/nginx:latest\n",
			"my-cluster-worker2": "docker.io/library/nginx:latest\n" +
				"docker.io/library/redis:alpine\n",
		},
		exportNode: "my-cluster-worker2",
		tmpPath:    kindExporterTarPath,
		requested:  []string{"nginx:latest", "redis:alpine", "busybox:latest"},
		expectedExport: []string{
			"docker.io/library/nginx:latest",
			"docker.io/library/redis:alpine",
			"docker.io/library/busybox:latest",
		},
	})
}

// K3d agents are probed like Kind workers.
func TestExportK3dUsesAgentHoldingRequestedImage(t *testing.T) {
	t.Parallel()

	runMultiNodeExportTest(t, multiNodeExportCase{
		distribution: v1alpha1.DistributionK3s,
		nodes: []container.Summary{
			{
				Names:  []string{"/k3d-my-cluster-serverlb"},
				Labels: map[string]string{"k3d.role": "loadbalancer"},
			},
			{
				Names:  []string{"/k3d-my-cluster-agent-0"},
				Labels: map[string]string{"k3d.role": "agent"},
			},
			{
				Names:  []string{"/k3d-my-cluster-server-0"},
				Labels: map[string]string{"k3d.role": "server"},
			},
		},
		imageLists: map[string]string{
			"k3d-my-cluster-server-0": "",
			"k3d-my-cluster-agent-0":  "docker.io/traefik/whoami:v1.10\n",
		},
		exportNode:     "k3d-my-cluster-agent-0",
		tmpPath:        "/tmp/ksail-images-export.tar",
		requested:      []string{"traefik/whoami:v1.10"},
		expectedExport: []string{"docker.io/traefik/whoami:v1.10"},
	})
}

// Exporting everything picks the node holding the most images.
func TestExportAllUsesNodeHoldingMostImages(t *testing.T) {
	t.Parallel()

	runMultiNodeExportTest(t, multiNodeExportCase{
		distribution: v1alpha1.DistributionVanilla,
		nodes:        kindMultiNodeSummaries()[:2],
		imageLists: map[string]string{
			"my-cluster-control-plane": "registry.k8s.io/pause:3.10\n",
			"my-cluster-worker": "registry.k8s.io/pause:3.10\n" +
				"docker.io/traefik/whoami:v1.10\n",
		},
		exportNode: "my-cluster-worker",
		tmpPath:    kindExporterTarPath,
		expectedExport: []string{
			"registry.k8s.io/pause:3.10",
			"docker.io/traefik/whoami:v1.10",
		},
	})
}
