package eksinventory_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/internal/maintenance/eksinventory"
)

func TestAWSBoundaryRejectsMutationsBeforeStartingAProcess(t *testing.T) {
	if os.Getenv("KSAIL_EKS_TEST_PROCESS") != "" {
		helperProcess()

		return
	}

	dir := prepareAWSProcess(t)
	marker := filepath.Join(dir, "invoked")

	_, err := eksinventory.QueryAWSForTest(
		context.Background(),
		"ec2",
		"delete-volume",
		"--region",
		"eu-central-1",
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
		"{}",
		"--no-cli-pager",
	)
	if err == nil {
		t.Fatal("mutation was accepted")
	}

	_, err = os.Stat(marker)
	if !os.IsNotExist(err) {
		t.Fatal("forbidden operation started a process")
	}

	t.Setenv("KSAIL_EKS_TEST_PROCESS", "timeout")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	data, err := eksinventory.QueryAWSForTest(
		ctx,
		"ec2",
		"describe-volumes",
		"--region",
		"eu-central-1",
		"--output",
		"json",
		"--no-paginate",
		"--cli-input-json",
		"{}",
		"--no-cli-pager",
	)
	if err == nil || len(data) != 0 {
		t.Fatal("timed-out process retained plausible stdout")
	}
}

func prepareAWSProcess(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	err = os.Symlink(executable, filepath.Join(dir, "aws"))
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("KSAIL_EKS_TEST_PROCESS", "invoked")

	t.Setenv("KSAIL_EKS_TEST_DIR", dir)

	return dir
}

func helperProcess() {
	if os.Getenv("KSAIL_EKS_TEST_PROCESS") == "timeout" {
		_, _ = fmt.Fprintln(os.Stdout, `{"Volumes":[]}`)

		time.Sleep(time.Minute)
		os.Exit(0)
	}

	root, err := os.OpenRoot(os.Getenv("KSAIL_EKS_TEST_DIR"))
	if err != nil {
		os.Exit(1)
	}

	err = root.WriteFile("invoked", []byte("invoked"), 0o600)
	if err != nil {
		os.Exit(1)
	}

	err = root.Close()
	if err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}
