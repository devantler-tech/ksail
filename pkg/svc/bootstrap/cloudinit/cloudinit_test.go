package cloudinitbootstrap_test

import (
	"strings"
	"testing"

	cloudinitbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/cloudinit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// parsedConfig mirrors the cloud-config subset BuildUserData emits, so tests can
// assert on the parsed document rather than on brittle substrings.
type parsedConfig struct {
	WriteFiles []struct {
		Path        string `yaml:"path"`
		Permissions string `yaml:"permissions"`
		Content     string `yaml:"content"`
	} `yaml:"write_files"` //nolint:tagliatelle // cloud-init's schema mandates the snake_case key.
	RunCmd [][]string `yaml:"runcmd"`
}

// parse asserts the document carries the cloud-config header and is valid YAML,
// returning the decoded subset.
func parse(t *testing.T, userData string) parsedConfig {
	t.Helper()

	require.True(t,
		strings.HasPrefix(userData, "#cloud-config\n"),
		"user_data must begin with the #cloud-config header",
	)

	var cfg parsedConfig

	require.NoError(t, yaml.Unmarshal([]byte(userData), &cfg), "user_data must be valid YAML")

	return cfg
}

func TestBuildUserDataDefaults(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo first", "echo second"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	file := cfg.WriteFiles[0]
	assert.Equal(t, cloudinitbootstrap.DefaultScriptPath, file.Path)
	assert.Equal(t, "0700", file.Permissions)

	// The script runs the bootstrap directly (argv form) so its exit code is
	// preserved by cloud-init.
	assert.Equal(t, [][]string{{"/bin/sh", cloudinitbootstrap.DefaultScriptPath}}, cfg.RunCmd)

	// Strict mode + an exec redirect to the (shell-quoted) default log, then the
	// commands in order.
	assert.Equal(t,
		"#!/bin/sh\n"+
			"set -eu\n"+
			"exec >> '"+cloudinitbootstrap.DefaultLogPath+"' 2>&1\n"+
			"echo first\n"+
			"echo second\n",
		file.Content,
	)
}

func TestBuildUserDataCustomPaths(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands:   []string{"echo hi"},
		ScriptPath: "/opt/ksail/boot.sh",
		LogPath:    "/var/log/custom.log",
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Equal(t, "/opt/ksail/boot.sh", cfg.WriteFiles[0].Path)
	assert.Equal(t, [][]string{{"/bin/sh", "/opt/ksail/boot.sh"}}, cfg.RunCmd)
	assert.Contains(t, cfg.WriteFiles[0].Content, "exec >> '/var/log/custom.log' 2>&1\n")
}

func TestBuildUserDataQuotesLogPath(t *testing.T) {
	t.Parallel()

	// A log path with shell metacharacters (a space, and an injection attempt) must
	// be neutralised by quoting: it stays a single literal redirect target and
	// cannot break the redirect or run an extra command at first boot.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		LogPath:  "/var/log/x; touch /tmp/pwned",
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Contains(t,
		cfg.WriteFiles[0].Content,
		"exec >> '/var/log/x; touch /tmp/pwned' 2>&1\n",
	)
	assert.NotContains(t, cfg.WriteFiles[0].Content, "touch /tmp/pwned\n")
}

func TestBuildUserDataDropsBlankCommands(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"", "echo kept", "   ", "\t"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	// Only the one non-blank command survives; no empty script lines are emitted.
	assert.Equal(t,
		"#!/bin/sh\n"+
			"set -eu\n"+
			"exec >> '"+cloudinitbootstrap.DefaultLogPath+"' 2>&1\n"+
			"echo kept\n",
		cfg.WriteFiles[0].Content,
	)
}

func TestBuildUserDataPreservesShellMetacharacters(t *testing.T) {
	t.Parallel()

	// A realistic k3s install command (pipes, quotes, env assignment) must survive
	// YAML round-tripping byte-for-byte.
	command := "script=\"$(curl -sfL 'https://get.k3s.io')\" && printf '%s' \"$script\" | " +
		"INSTALL_K3S_VERSION='v1.30.2+k3s1' K3S_TOKEN='secret' sh -s - server --cluster-init"

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{command},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Contains(t, cfg.WriteFiles[0].Content, "\n"+command+"\n")
}

func TestBuildUserDataDeterministic(t *testing.T) {
	t.Parallel()

	cfg := cloudinitbootstrap.Config{Commands: []string{"echo a", "echo b", "echo c"}}

	first, err := cloudinitbootstrap.BuildUserData(cfg)
	require.NoError(t, err)

	second, err := cloudinitbootstrap.BuildUserData(cfg)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}

func TestBuildUserDataErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     cloudinitbootstrap.Config
		wantErr error
	}{
		{
			name:    "no commands",
			cfg:     cloudinitbootstrap.Config{},
			wantErr: cloudinitbootstrap.ErrNoCommands,
		},
		{
			name:    "only blank commands",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"", "  ", "\t\n"}},
			wantErr: cloudinitbootstrap.ErrInvalidCommand, // the "\t\n" newline is rejected first
		},
		{
			name:    "all-whitespace without newline",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"", "   "}},
			wantErr: cloudinitbootstrap.ErrNoCommands,
		},
		{
			name:    "command with embedded newline",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"echo a\necho b"}},
			wantErr: cloudinitbootstrap.ErrInvalidCommand,
		},
		{
			name:    "command with NUL byte",
			cfg:     cloudinitbootstrap.Config{Commands: []string{"echo \x00"}},
			wantErr: cloudinitbootstrap.ErrInvalidCommand,
		},
		{
			name: "relative script path",
			cfg: cloudinitbootstrap.Config{
				Commands:   []string{"echo hi"},
				ScriptPath: "relative/boot.sh",
			},
			wantErr: cloudinitbootstrap.ErrPathNotAbsolute,
		},
		{
			name: "relative log path",
			cfg: cloudinitbootstrap.Config{
				Commands: []string{"echo hi"},
				LogPath:  "relative.log",
			},
			wantErr: cloudinitbootstrap.ErrPathNotAbsolute,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out, err := cloudinitbootstrap.BuildUserData(testCase.cfg)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Empty(t, out, "no document should be returned on error")
		})
	}
}

func TestBuildUserDataShellQuotesSingleQuoteInLogPath(t *testing.T) {
	t.Parallel()

	// A log path containing a single quote must be escaped via the POSIX '\''
	// idiom so it does not terminate the surrounding single-quoted string.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		LogPath:  "/var/log/it's-here.log",
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	// The redirect line must contain the escaped single-quote sequence '\'' so
	// the shell sees the path as a single literal token.
	assert.Contains(t,
		cfg.WriteFiles[0].Content,
		`exec >> '/var/log/it'\''s-here.log' 2>&1`,
		"single quote in log path must be escaped via the POSIX '\\'' idiom",
	)
}

func TestBuildUserDataLogPathWithSpace(t *testing.T) {
	t.Parallel()

	// A log path with a space must be kept as one redirect target (not split
	// into two tokens) when the shell evaluates the exec line.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
		LogPath:  "/var/log/k sail bootstrap.log",
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Contains(t,
		cfg.WriteFiles[0].Content,
		"exec >> '/var/log/k sail bootstrap.log' 2>&1\n",
		"log path with a space must be wrapped in single quotes as a single token",
	)
}

func TestBuildUserDataCommandsWithWhitespace(t *testing.T) {
	t.Parallel()

	// Commands with leading or trailing whitespace are not blank: they must be
	// kept verbatim, not dropped like whitespace-only entries.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"  echo leading", "echo trailing  "},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	content := cfg.WriteFiles[0].Content
	assert.Contains(t, content, "\n  echo leading\n",
		"command with leading whitespace must be kept verbatim")
	assert.Contains(t, content, "\necho trailing  \n",
		"command with trailing whitespace must be kept verbatim")
}

func TestBuildUserDataSingleCommand(t *testing.T) {
	t.Parallel()

	// A single command must produce exactly four lines: shebang, set flags,
	// exec redirect, and the command itself.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo only"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	assert.Equal(t,
		"#!/bin/sh\n"+
			"set -eu\n"+
			"exec >> '"+cloudinitbootstrap.DefaultLogPath+"' 2>&1\n"+
			"echo only\n",
		cfg.WriteFiles[0].Content,
	)
}

func TestBuildUserDataScriptEndsWithExactlyOneNewline(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo a", "echo b"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	content := cfg.WriteFiles[0].Content
	assert.True(t, strings.HasSuffix(content, "\n"),
		"script must end with a newline")
	assert.False(t, strings.HasSuffix(content, "\n\n"),
		"script must end with exactly one newline, not two")
}

func TestBuildUserDataRunCmdUsesShBinary(t *testing.T) {
	t.Parallel()

	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo hi"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.RunCmd, 1)
	require.Len(t, cfg.RunCmd[0], 2)
	assert.Equal(t, "/bin/sh", cfg.RunCmd[0][0],
		"runcmd must use /bin/sh so the script's shebang is the actual interpreter")
}

func TestBuildUserDataBothPathsRelative(t *testing.T) {
	t.Parallel()

	// Both ScriptPath and LogPath are relative: the combined check rejects the
	// config with ErrPathNotAbsolute.
	out, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands:   []string{"echo hi"},
		ScriptPath: "boot.sh",
		LogPath:    "boot.log",
	})
	require.ErrorIs(t, err, cloudinitbootstrap.ErrPathNotAbsolute)
	assert.Empty(t, out)
}

func TestBuildUserDataZeroValueConfig(t *testing.T) {
	t.Parallel()

	// A zero-value Config (no Commands set at all) must fail with ErrNoCommands.
	out, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{})
	require.ErrorIs(t, err, cloudinitbootstrap.ErrNoCommands)
	assert.Empty(t, out)
}

func TestBuildUserDataScriptHeaderOrder(t *testing.T) {
	t.Parallel()

	// The first three non-empty lines of the script must be shebang, set flags,
	// exec redirect — in that exact order — regardless of the commands that
	// follow.
	userData, err := cloudinitbootstrap.BuildUserData(cloudinitbootstrap.Config{
		Commands: []string{"echo cmd"},
	})
	require.NoError(t, err)

	cfg := parse(t, userData)

	require.Len(t, cfg.WriteFiles, 1)
	lines := strings.SplitN(cfg.WriteFiles[0].Content, "\n", 5)
	require.GreaterOrEqual(t, len(lines), 4)
	assert.Equal(t, "#!/bin/sh", lines[0])
	assert.Equal(t, "set -eu", lines[1])
	assert.True(t, strings.HasPrefix(lines[2], "exec >> '"), "third line must be the exec redirect")
	assert.True(t, strings.HasSuffix(lines[2], "2>&1"), "exec redirect must capture stderr")
}
