package codeqlprofile

import (
	"errors"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

// CgroupLimits records inherited cgroup-v2 constraints without exporting paths.
// A nil limit is explicitly unlimited only when Verified is true.
type CgroupLimits struct {
	MemoryBytes *uint64 `json:"memoryBytes"`
	SwapBytes   *uint64 `json:"swapBytes"`
	Verified    bool    `json:"verified"`
}

// ReadCgroupLimits includes ancestor constraints. Missing or unsupported reads
// remain unknown; kernel-supplied paths cannot escape the opened hierarchy.
func ReadCgroupLimits(procRoot, hierarchy string) (CgroupLimits, error) {
	group, err := unifiedCgroup(procRoot)
	if err != nil {
		return CgroupLimits{}, err
	}

	root, err := os.OpenRoot(hierarchy)
	if err != nil {
		return CgroupLimits{}, errMeasurement
	}

	defer func() { _ = root.Close() }()

	controllers, err := root.ReadFile("cgroup.controllers")
	if err != nil || !slices.Contains(strings.Fields(string(controllers)), "memory") {
		return CgroupLimits{}, errMeasurement
	}

	var limits CgroupLimits

	for directory := strings.TrimPrefix(group, "/"); ; directory = path.Dir(directory) {
		atRoot := directory == "" || directory == "."

		err = inheritCgroupDirectory(root, directory, atRoot, &limits)
		if err != nil {
			return CgroupLimits{}, err
		}

		if atRoot {
			break
		}
	}

	limits.Verified = true

	return limits, nil
}

func inheritCgroupDirectory(
	root *os.Root,
	directory string,
	atRoot bool,
	limits *CgroupLimits,
) error {
	for name, target := range map[string]**uint64{
		"memory.max":      &limits.MemoryBytes,
		"memory.swap.max": &limits.SwapBytes,
	} {
		value, err := root.ReadFile(path.Join(directory, name))
		// cgroup-v2's root has no local resource-control files; ancestors
		// below it must provide explicit values for a verified observation.
		if errors.Is(err, os.ErrNotExist) && atRoot {
			continue
		}

		if err != nil {
			return errMeasurement
		}

		text := strings.TrimSpace(string(value))
		if text == "max" {
			continue
		}

		limit, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return errMeasurement
		}

		if *target == nil || limit < **target {
			*target = &limit
		}
	}

	return nil
}

func unifiedCgroup(procRoot string) (string, error) {
	root, err := os.OpenRoot(procRoot)
	if err != nil {
		return "", errMeasurement
	}
	defer func() { _ = root.Close() }()

	contents, err := root.ReadFile("self/cgroup")
	if err != nil {
		return "", errMeasurement
	}

	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "0::/") {
		return "", errMeasurement
	}

	group := strings.TrimPrefix(lines[0], "0::")
	if path.Clean(group) != group || strings.ContainsAny(group, "\x00\r\n") {
		return "", errMeasurement
	}

	return group, nil
}
