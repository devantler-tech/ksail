package kernelmod_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kernelmod"
	"github.com/stretchr/testify/assert"
)

type containsModuleTestCase struct {
	name           string
	modulesContent string
	moduleName     string
	want           bool
}

func runContainsModuleTests(t *testing.T, tests []containsModuleTestCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := kernelmod.ContainsModule(tt.modulesContent, tt.moduleName)
			assert.Equal(t, tt.want, got, "unexpected result for module %q", tt.moduleName)
		})
	}
}

// TestContainsModule tests basic presence and absence cases.
func TestContainsModule(t *testing.T) {
	t.Parallel()

	runContainsModuleTests(t, []containsModuleTestCase{
		{
			name: "module present at start",
			modulesContent: `br_netfilter 32768 0 - Live 0x0000000000000000
bridge 286720 1 br_netfilter, Live 0x0000000000000000
stp 16384 1 bridge, Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       true,
		},
		{
			name: "module present in middle",
			modulesContent: `xt_nat 16384 0 - Live 0x0000000000000000
br_netfilter 32768 0 - Live 0x0000000000000000
bridge 286720 1 br_netfilter, Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       true,
		},
		{
			name: "module not present",
			modulesContent: `xt_nat 16384 0 - Live 0x0000000000000000
bridge 286720 1 - Live 0x0000000000000000
stp 16384 1 bridge, Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       false,
		},
		{
			name:           "empty content",
			modulesContent: "",
			moduleName:     "br_netfilter",
			want:           false,
		},
		{
			name:           "only newlines",
			modulesContent: "\n\n\n",
			moduleName:     "br_netfilter",
			want:           false,
		},
	})
}

// TestContainsModule_ExactMatching tests exact first-field matching to avoid false positives.
func TestContainsModule_ExactMatching(t *testing.T) {
	t.Parallel()

	runContainsModuleTests(t, []containsModuleTestCase{
		{
			name: "module present at end",
			modulesContent: `xt_nat 16384 0 - Live 0x0000000000000000
bridge 286720 1 br_netfilter, Live 0x0000000000000000
br_netfilter 32768 0 - Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       true,
		},
		{
			name: "module name as substring in dependency list",
			modulesContent: `bridge 286720 1 br_netfilter, Live 0x0000000000000000
stp 16384 1 bridge, Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       false,
		},
		{
			name: "similar module name but not exact match",
			modulesContent: `br_netfilter_ipv4 32768 0 - Live 0x0000000000000000
bridge 286720 1 - Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       false,
		},
		{
			name: "module with underscores",
			modulesContent: `xt_conntrack 16384 0 - Live 0x0000000000000000
ip_tables 32768 0 - Live 0x0000000000000000`,
			moduleName: "xt_conntrack",
			want:       true,
		},
		{
			name:           "single module single line",
			modulesContent: `br_netfilter 32768 0 - Live 0x0000000000000000`,
			moduleName:     "br_netfilter",
			want:           true,
		},
	})
}

// TestContainsModule_Formatting tests whitespace, empty names, and repeated entries.
func TestContainsModule_Formatting(t *testing.T) {
	t.Parallel()

	runContainsModuleTests(t, []containsModuleTestCase{
		{
			name:           "empty module name search",
			modulesContent: `br_netfilter 32768 0 - Live 0x0000000000000000`,
			moduleName:     "",
			want:           false,
		},
		{
			name: "whitespace handling",
			modulesContent: `  br_netfilter 32768 0 - Live 0x0000000000000000
bridge    286720 1 - Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       true,
		},
		{
			name: "trailing whitespace",
			modulesContent: `br_netfilter 32768 0 - Live 0x0000000000000000  
bridge 286720 1 - Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       true,
		},
		{
			name: "multiple matches should still work",
			modulesContent: `br_netfilter 32768 0 - Live 0x0000000000000000
bridge 286720 1 - Live 0x0000000000000000
br_netfilter 32768 0 - Live 0x0000000000000000`,
			moduleName: "br_netfilter",
			want:       true,
		},
	})
}

// TestContainsModule_EdgeCases tests additional edge cases with realistic /proc/modules format.
func TestContainsModule_EdgeCases(t *testing.T) {
	t.Parallel()

	realisticContent := `ccm 16384 0 - Live 0xffffffffc0b8e000
snd_seq 94208 1 snd_seq_midi, Live 0xffffffffc0b58000
xt_conntrack 16384 1 - Live 0xffffffffc0b4e000
nf_conntrack 184320 1 xt_conntrack, Live 0xffffffffc0af0000
br_netfilter 32768 0 - Live 0xffffffffc0ad0000
bridge 286720 1 br_netfilter, Live 0xffffffffc0a50000
stp 16384 1 bridge, Live 0xffffffffc0a43000`

	t.Run("realistic format with br_netfilter", func(t *testing.T) {
		t.Parallel()
		assert.True(t, kernelmod.ContainsModule(realisticContent, "br_netfilter"))
	})

	t.Run("realistic format with bridge", func(t *testing.T) {
		t.Parallel()
		assert.True(t, kernelmod.ContainsModule(realisticContent, "bridge"))
	})

	t.Run("realistic format with stp", func(t *testing.T) {
		t.Parallel()
		assert.True(t, kernelmod.ContainsModule(realisticContent, "stp"))
	})

	t.Run("realistic format without target", func(t *testing.T) {
		t.Parallel()
		assert.False(t, kernelmod.ContainsModule(realisticContent, "nonexistent_module"))
	})

	t.Run("case sensitivity check", func(t *testing.T) {
		t.Parallel()
		assert.False(t, kernelmod.ContainsModule(realisticContent, "BR_NETFILTER"))
	})
}
