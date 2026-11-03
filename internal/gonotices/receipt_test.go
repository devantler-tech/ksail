package gonotices_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/gonotices"
	"github.com/stretchr/testify/require"
)

func TestStageRefusesFalseReceiptProvenanceAndCounts(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"toolchain", "inputs", "negative-counts"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input, actual, release, _ := stageInputFixture(t)
			root, err := os.OpenRoot(input.Root)

			require.NoError(t, err)
			defer func() { require.NoError(t, root.Close()) }()

			data, err := root.ReadFile(".github/go-notice-receipts.json")
			require.NoError(t, err)

			switch scenario {
			case "toolchain":
				data = bytes.Replace(
					data,
					[]byte(`"toolchainVersion": "go1.27.3"`),
					[]byte(`"toolchainVersion": "go9.99.9"`),
					1,
				)
			case "inputs":
				start := bytes.Index(data, []byte(`"go.sum": "`)) + len(`"go.sum": "`)
				data = append(
					append(append([]byte{}, data[:start]...), []byte(strings.Repeat("a", 64))...),
					data[start+64:]...)
			case "negative-counts":
				data = bytes.ReplaceAll(data, []byte(`"packages": 1`), []byte(`"packages": -1`))
			}

			require.NoError(t, root.WriteFile(".github/go-notice-receipts.json", data, 0o600))

			_, err = gonotices.Stage(input.Root, release, actual)
			require.ErrorIs(t, err, gonotices.ErrIncomplete)
		})
	}
}

func TestReceiptBindsEveryTargetAndActualPayloadBytes(t *testing.T) {
	t.Parallel()
	input, _ := noticeInputFixture(t)
	payloads := receiptPayloadFixtures(t)
	require.NoError(t, gonotices.WriteReceipt(input.Root, payloads))
	require.NoError(t, gonotices.VerifyReceipt(input.Root, payloads))
	data, err := os.ReadFile(filepath.Join(input.Root, ".github/go-notice-receipts.json"))
	require.NoError(t, err)
	require.Less(t, len(data), 8192)

	for target := range payloads {
		require.Contains(t, string(data), target)
	}

	payload := payloads["cli-linux-amd64"]
	payload.Text = append(payload.Text, []byte("changed output")...)
	payloads["cli-linux-amd64"] = payload
	require.ErrorIs(t, gonotices.VerifyReceipt(input.Root, payloads), gonotices.ErrIncomplete)
}

func TestReceiptRefusesMissingExtraAndMismatchedTargetCoverage(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{"missing", "extra", "wrong-target", "missing-file"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			input, _ := noticeInputFixture(t)
			payloads := receiptPayloadFixtures(t)

			switch scenario {
			case "missing":
				delete(payloads, "desktop-linux-amd64")
			case "extra":
				payloads["cli-freebsd-amd64"] = payloads["cli-linux-amd64"]
			case "wrong-target":
				payloads["cli-linux-amd64"] = payloads["cli-windows-amd64"]
			case "missing-file":
				require.ErrorIs(
					t,
					gonotices.VerifyReceipt(input.Root, payloads),
					gonotices.ErrIncomplete,
				)

				return
			}

			require.ErrorIs(
				t,
				gonotices.WriteReceipt(input.Root, payloads),
				gonotices.ErrIncomplete,
			)
		})
	}
}

func receiptPayloadFixtures(t *testing.T) map[string]gonotices.Payload {
	t.Helper()

	payloads := map[string]gonotices.Payload{}
	for _, target := range []string{
		"cli-darwin-arm64", "cli-linux-amd64", "cli-linux-arm64", "cli-windows-amd64", "cli-windows-arm64",
		"desktop-darwin-arm64", "desktop-linux-amd64", "desktop-windows-amd64",
	} {
		payloads[target] = releasePayloadForTargetFixture(t, target)
	}

	return payloads
}
