package codeqlprofile_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

func TestParseTimeConvertsCommandMeasurements(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, platform, input string
		wantRSS               uint64
		wantSeconds           float64
	}{
		{
			"linux",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 5:24.91\n" +
				"Maximum resident set size (kbytes): 9177292\nExit status: 0\n",
			9397547008,
			324.91,
		},
		{
			"linux hours",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 1:02:03.5\n" +
				"Maximum resident set size (kbytes): 1024\nExit status: 0\n",
			1048576,
			3723.5,
		},
		{
			"darwin",
			"darwin",
			" 324.91 real 303.17 user 73.04 sys\n 9397547008 maximum resident set size\n",
			9397547008,
			324.91,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := codeqlprofile.ParseTime(testCase.platform, testCase.input)
			if err != nil || got.CommandPeakRSSBytes != testCase.wantRSS ||
				got.WallSeconds != testCase.wantSeconds {
				t.Fatalf(
					"got %+v, %v; want RSS=%d seconds=%g",
					got,
					err,
					testCase.wantRSS,
					testCase.wantSeconds,
				)
			}
		})
	}
}

func TestParseTimeRejectsPartialOrAmbiguousMeasurements(t *testing.T) {
	t.Parallel()

	valid := "Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01.25\n" +
		"Maximum resident set size (kbytes): 1024\nExit status: 0\n"

	cases := []struct{ name, platform, input string }{
		{"missing elapsed", "linux", "Maximum resident set size (kbytes): 1024\nExit status: 0\n"},
		{
			"missing RSS",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01.25\nExit status: 0\n",
		},
		{
			"missing exit",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01.25\n" +
				"Maximum resident set size (kbytes): 1024\n",
		},
		{"duplicate field", "linux", valid + "Maximum resident set size (kbytes): 2048\n"},
		{
			"RSS overflow",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01\n" +
				"Maximum resident set size (kbytes): 18446744073709551615\nExit status: 0\n",
		},
		{
			"zero RSS",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 0:01\n" +
				"Maximum resident set size (kbytes): 0\nExit status: 0\n",
		},
		{
			"invalid minutes",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 1:60:00\n" +
				"Maximum resident set size (kbytes): 1024\nExit status: 0\n",
		},
		{
			"invalid seconds",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 0:60\n" +
				"Maximum resident set size (kbytes): 1024\nExit status: 0\n",
		},
		{
			"nonfinite elapsed",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): 0:NaN\n" +
				"Maximum resident set size (kbytes): 1024\nExit status: 0\n",
		},
		{
			"negative elapsed",
			"linux",
			"Elapsed (wall clock) time (h:mm:ss or m:ss): -1:00\n" +
				"Maximum resident set size (kbytes): 1024\nExit status: 0\n",
		},
		{"wrong platform", "windows", valid},
		{"empty", "darwin", ""},
	}
	rejectTimeCases(t, cases)
}

func rejectTimeCases(t *testing.T, cases []struct{ name, platform, input string }) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := codeqlprofile.ParseTime(testCase.platform, testCase.input)
			if err == nil {
				t.Fatal("incomplete or invalid measurement accepted")
			}
		})
	}
}

func TestSummarizeRequiresEveryExpectedExtraction(t *testing.T) {
	t.Parallel()

	good := codeqlprofile.Record{
		Project: "root",
		Stats:   codeqlprofile.Stats{CommandPeakRSSBytes: 1048576, WallSeconds: 1.25},
	}

	got, err := codeqlprofile.Summarize(
		[]string{"root", "third_party/otelzap"},
		[]codeqlprofile.Record{good},
	)
	if err == nil || got.Complete || len(got.Records) != 1 {
		t.Fatalf("partial observations became complete or were lost: %+v, %v", got, err)
	}

	second := good
	second.Project = "third_party/otelzap"

	got, err = codeqlprofile.Summarize(
		[]string{"root", "third_party/otelzap"},
		[]codeqlprofile.Record{second, good},
	)
	if err != nil || !got.Complete || len(got.Records) != 2 || got.Records[0].Project != "root" {
		t.Fatalf("complete observations not ordered by inventory: %+v, %v", got, err)
	}
}

func TestSummarizeRetainsFailedObservationsWithoutClearingThem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		expected []string
		records  []codeqlprofile.Record
	}{
		{
			"failed process",
			[]string{"root"},
			[]codeqlprofile.Record{
				{
					Project:  "root",
					ExitCode: 137,
					Stats:    codeqlprofile.Stats{CommandPeakRSSBytes: 1048576, WallSeconds: 1.25},
				},
			},
		},
		{
			"failed time status",
			[]string{"root"},
			[]codeqlprofile.Record{
				{
					Project: "root",
					Stats: codeqlprofile.Stats{
						CommandPeakRSSBytes: 1048576,
						WallSeconds:         1.25,
						ExitCode:            42,
					},
				},
			},
		},
		{
			"duplicate observation",
			[]string{"root"},
			[]codeqlprofile.Record{{Project: "root"}, {Project: "root"}},
		},
		{"unexpected project", []string{"root"}, []codeqlprofile.Record{{Project: "other"}}},
		{"duplicate expected", []string{"root", "root"}, nil},
		{"empty inventory", nil, nil},
		{"invalid project name", []string{"root\n::error::injected"}, nil},
		{"missing measurements", []string{"root"}, []codeqlprofile.Record{{Project: "root"}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := codeqlprofile.Summarize(testCase.expected, testCase.records)
			if err == nil || got.Complete || len(got.Records) != len(testCase.records) {
				t.Fatalf("invalid or failed observation cleared/discarded: %+v, %v", got, err)
			}
		})
	}
}
