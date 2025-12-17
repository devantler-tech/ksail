package cipher

import (
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	"github.com/spf13/cobra"
)

// NewCipherCmd creates the cipher command that integrates with SOPS.
func NewCipherCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cipher",
		Short: "Manage encrypted files with SOPS",
		Long: `Cipher command provides access to SOPS (Secrets OPerationS) functionality
for encrypting and decrypting files.

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault`,
		SilenceUsage: true,
	}

	// Add subcommands
	cmd.AddCommand(NewEncryptCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewDecryptCmd())

	return cmd
}
