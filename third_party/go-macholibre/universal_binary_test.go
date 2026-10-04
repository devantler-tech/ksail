package macho

import (
	"debug/macho"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractBinariesTo(t *testing.T) {
	//runMakeTarget(t, "fixture-ls")

	tests := []struct {
		name                string
		universalBinaryPath string
		expected            []ExtractedFile
	}{
		{
			name:                "extract binaries from universal binary",
			universalBinaryPath: asset(t, "ls_universal_signed"),
			expected: []ExtractedFile{
				{
					Path: asset(t, "ls_amd64_signed"),
					UniversalArchInfo: UniversalArchInfo{
						CPU: macho.CpuAmd64,
					},
				},
				{
					Path: asset(t, "ls_arm64e_signed"),
					UniversalArchInfo: UniversalArchInfo{
						CPU: macho.CpuArm64,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			f, err := os.Open(tt.universalBinaryPath)
			require.NoError(t, err)

			actual, err := Extract(f, dir)
			require.NoError(t, err)
			require.Len(t, actual, len(tt.expected))

			// assert each file is a macho binary
			for idx, a := range actual {
				// note: we don't compare the subCPU
				assert.Equal(t, a.UniversalArchInfo.CPU, tt.expected[idx].UniversalArchInfo.CPU)

				// ... assert the file exists
				actualFile, err := os.Open(a.Path)
				require.NoError(t, err)

				// ... assert the file is a macho-formatted file
				_, err = macho.NewFile(actualFile)
				require.NoError(t, err)

				actualContents, err := io.ReadAll(actualFile)
				require.NoError(t, err)

				expectedContents, err := os.ReadFile(tt.expected[idx].Path)
				require.NoError(t, err)

				// ... assert the extracted file matches that of lipo-extracted files
				assert.True(t, cmp.Equal(expectedContents, actualContents))
			}

		})
	}
}

func TestExtractReaders(t *testing.T) {
	tests := []struct {
		name                string
		universalBinaryPath string
		expected            []ExtractedReader
		wantErr             require.ErrorAssertionFunc
	}{
		{
			name:                "file not found",
			universalBinaryPath: "/tick/tick/boom",
			wantErr:             require.Error,
		},
		{
			name:                "extract binaries from universal binary",
			universalBinaryPath: asset(t, "ls_universal_signed"),
			wantErr:             require.NoError,
			expected: []ExtractedReader{
				{
					UniversalArchHeader: UniversalArchHeader{
						UniversalArchInfo: UniversalArchInfo{
							CPU: macho.CpuAmd64,
						},
						Offset: 0x4000,
						Size:   0x11c60,
					},
				},
				{
					UniversalArchHeader: UniversalArchHeader{
						UniversalArchInfo: UniversalArchInfo{
							CPU: macho.CpuArm64,
						},
						Offset: 0x18000,
						Size:   0x15aa0,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(tt.universalBinaryPath)
			tt.wantErr(t, err)

			actual, err := ExtractReaders(f)
			tt.wantErr(t, err)
			require.Len(t, actual, len(tt.expected))

			// assert each file is a macho binary
			for idx, a := range actual {
				// note: we don't compare the subCPU
				assert.Equal(t, tt.expected[idx].UniversalArchInfo.CPU, a.CPU)
				assert.Equal(t, tt.expected[idx].UniversalArchHeader.Offset, a.Offset)
				assert.Equal(t, tt.expected[idx].UniversalArchHeader.Size, a.Size)
			}
		})
	}
}

func TestPackageUniversalBinary(t *testing.T) {
	//runMakeTarget(t, "fixture-ls")

	tests := []struct {
		name       string
		binaryPath string
	}{
		{
			name:       "repackage binaries from universal binary",
			binaryPath: asset(t, "ls_universal_signed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			f, err := os.Open(tt.binaryPath)
			require.NoError(t, err)

			actual, err := Extract(f, dir)

			final := path.Join(dir, "universal")

			var filePaths []string
			for _, a := range actual {
				filePaths = append(filePaths, a.Path)
			}

			err = Package(final, filePaths...)
			require.NoError(t, err)

			newF, err := os.Open(final)
			require.NoError(t, err)

			_, err = macho.NewFatFile(newF)
			require.NoError(t, err)

			actualContents, err := io.ReadAll(newF)
			require.NoError(t, err)

			expectedContents, err := os.ReadFile(tt.binaryPath)
			require.NoError(t, err)

			// ... assert we could create a universal binary from the extracted binaries
			assert.True(t, cmp.Equal(expectedContents, actualContents))
		})
	}
}

func TestIsUniversalMachoBinary(t *testing.T) {
	// runMakeTarget(t, "fixture-ls")
	// runMakeTarget(t, "fixture-non-mach-o")

	tests := []struct {
		name       string
		binaryPath string
		expected   bool
	}{
		{
			name:       "positive case",
			binaryPath: asset(t, "ls_universal_signed"),
			expected:   true,
		},
		{
			name:       "negative case",
			binaryPath: asset(t, "ls_amd64_signed"),
			expected:   false,
		},
		{
			name:       "negative case bin from different platform",
			binaryPath: asset(t, "linux_amd64"),
			expected:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.Open(tt.binaryPath)
			require.NoError(t, err)
			assert.Equalf(t, tt.expected, IsUniversalMachoBinary(f), "IsUniversalMachoBinary(%v)", tt.binaryPath)
		})
	}
}

//// make will run the default make target for the given test fixture path
//func runMakeTarget(t *testing.T, fixtureName string) {
//	cwd, err := os.Getwd()
//	if err != nil {
//		t.Errorf("unable to get cwd: %+v", err)
//	}
//	fixtureDir := filepath.Join(cwd, "test-fixtures/", fixtureName)
//
//	t.Logf("Generating Fixture in %q", fixtureDir)
//
//	cmd := exec.Command("make")
//	cmd.Dir = fixtureDir
//
//	stderr, err := cmd.StderrPipe()
//	if err != nil {
//		t.Fatalf("could not get stderr: %+v", err)
//	}
//	stdout, err := cmd.StdoutPipe()
//	if err != nil {
//		t.Fatalf("could not get stdout: %+v", err)
//	}
//
//	err = cmd.Start()
//	if err != nil {
//		t.Fatalf("failed to start cmd: %+v", err)
//	}
//
//	show := func(label string, reader io.ReadCloser) {
//		scanner := bufio.NewScanner(reader)
//		scanner.Split(bufio.ScanLines)
//		for scanner.Scan() {
//			t.Logf("%s: %s", label, scanner.Text())
//		}
//	}
//	go show("out", stdout)
//	go show("err", stderr)
//
//	if err := cmd.Wait(); err != nil {
//		if exiterr, ok := err.(*exec.ExitError); ok {
//			// The program has exited with an exit code != 0
//
//			// This works on both Unix and Windows. Although package
//			// syscall is generally platform dependent, WaitStatus is
//			// defined for both Unix and Windows and in both cases has
//			// an ExitStatus() method with the same signature.
//			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
//				if status.ExitStatus() != 0 {
//					t.Fatalf("failed to generate fixture: rc=%d", status.ExitStatus())
//				}
//			}
//		} else {
//			t.Fatalf("unable to get generate fixture result: %+v", err)
//		}
//	}
//}

// asset returns the path to the cached asset file for a generated test fixture
func asset(t *testing.T, assetName string) string {
	assetPath := filepath.Join("test-fixtures", "assets", assetName)
	if _, err := os.Stat(assetPath); os.IsNotExist(err) {
		t.Fatalf("unable to find fixture %q", assetPath)
	}
	return assetPath
}
