package codeqlprofile_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/codeqlprofile"
)

const memoryFixture = "MemTotal: 16384 kB\nMemAvailable: 4096 kB\nSwapTotal: 8192 kB\nSwapFree: 2048 kB\n"

func TestParseMemoryRetainsNativeUnits(t *testing.T) {
	t.Parallel()

	sample, err := codeqlprofile.ParseMemory(memoryFixture, "1024")

	want := codeqlprofile.MemorySample{
		TotalBytes: 16777216, AvailableBytes: 4194304, SwapTotalBytes: 8388608,
		SwapUsedBytes: 6291456, LargestRSSBytes: 1048576,
	}
	if err != nil || !reflect.DeepEqual(sample, want) {
		t.Fatalf("wrong native memory observation: %+v, %v", sample, err)
	}
}

func TestParseMemoryRequiresCompleteNativeFields(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"missing":                 strings.ReplaceAll(memoryFixture, "SwapFree: 2048 kB\n", ""),
		"partial":                 memoryFixture + "MemTotal: 16384 kB\n",
		"bad unit":                strings.ReplaceAll(memoryFixture, "2048 kB", "2048 MB"),
		"negative":                strings.ReplaceAll(memoryFixture, "2048", "-1"),
		"overflow":                strings.ReplaceAll(memoryFixture, "16384", "18446744073709551615"),
		"free exceeds total":      strings.ReplaceAll(memoryFixture, "2048", "8193"),
		"available exceeds total": strings.ReplaceAll(memoryFixture, "4096", "16385"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := codeqlprofile.ParseMemory(input, "1024")
			if err == nil {
				t.Fatalf("invalid observation accepted: %s", name)
			}
		})
	}

	for _, rss := range []string{"", "x", "-1", "18446744073709551615"} {
		_, err := codeqlprofile.ParseMemory(memoryFixture, rss)
		if err == nil {
			t.Fatalf("accepted RSS %q", rss)
		}
	}
}

func TestObserveMemoryRetainsSampledExtremaAndRejectsInvalidSuccessors(t *testing.T) {
	t.Parallel()

	sample, err := codeqlprofile.ParseMemory(memoryFixture, "1024")
	if err != nil {
		t.Fatal(err)
	}

	first, err := codeqlprofile.ObserveMemory(codeqlprofile.MemorySummary{}, 0, sample)
	if err != nil {
		t.Fatal(err)
	}

	next := sample
	next.AvailableBytes = 1024
	next.SwapUsedBytes = 7000000
	next.LargestRSSBytes = 2000000

	second, err := codeqlprofile.ObserveMemory(first, 5, next)

	want := codeqlprofile.MemorySummary{
		SampleCount: 2, LastElapsedSeconds: 5, TotalBytes: 16777216, SwapTotalBytes: 8388608,
		MinimumAvailableBytes: 1024, MaximumSwapUsedBytes: 7000000, LargestSampledRSSBytes: 2000000,
	}
	if err != nil || !reflect.DeepEqual(second, want) {
		t.Fatalf("sample extrema lost: %+v, %v", second, err)
	}

	for _, elapsed := range []float64{-1, 0, math.NaN(), math.Inf(1)} {
		_, err := codeqlprofile.ObserveMemory(first, elapsed, sample)
		if err == nil {
			t.Fatal("unordered/nonfinite time accepted")
		}
	}

	next.TotalBytes++

	_, err = codeqlprofile.ObserveMemory(second, 10, next)
	if err == nil {
		t.Fatal("changing memory envelope accepted")
	}

	next = sample

	next.SwapTotalBytes++

	_, err = codeqlprofile.ObserveMemory(second, 10, next)
	if err == nil {
		t.Fatal("changing swap envelope accepted")
	}
}
