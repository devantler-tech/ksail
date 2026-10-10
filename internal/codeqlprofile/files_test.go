package codeqlprofile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

func TestReadMeasurementsKeepsSuccessfulAndFailedProcesses(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeMeasurement(t, directory, "00000", "0")
	writeMeasurement(t, directory, "00001", "137")

	report, err := codeqlprofile.ReadMeasurements(
		"linux",
		directory,
		[]string{"root", "root-desktop", "missing"},
	)
	if err == nil || report.Complete || len(report.Records) != 2 {
		t.Fatalf("partial run was cleared or discarded: %+v, %v", report, err)
	}

	if report.Records[1].ExitCode != 137 || report.Records[1].Stats.CommandPeakRSSBytes != 1048576 {
		t.Fatalf("failed process measurements lost: %+v", report.Records[1])
	}
}

func TestReadMeasurementsRequiresValidProcessAndTimeResults(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"0", "not-a-status", "256", "-1", ""} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			writeMeasurement(t, directory, "00000", status)

			report, err := codeqlprofile.ReadMeasurements("linux", directory, []string{"root"})
			if status == "0" {
				if err != nil || !report.Complete {
					t.Fatalf("complete run rejected: %+v, %v", report, err)
				}

				return
			}

			if err == nil || report.Complete {
				t.Fatalf("invalid process status accepted: %+v, %v", report, err)
			}
		})
	}
}

func writeMeasurement(t *testing.T, directory, index, status string) {
	t.Helper()

	contents := map[string]string{
		".time": "Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01.25\n" +
			"Maximum resident set size (kbytes): 1024\nExit status: 0\n",
		".exit": status + "\n",
	}

	for suffix, content := range contents {
		err := os.WriteFile(filepath.Join(directory, index+suffix), []byte(content), 0o600)
		if err != nil {
			t.Fatal(err)
		}
	}
}
