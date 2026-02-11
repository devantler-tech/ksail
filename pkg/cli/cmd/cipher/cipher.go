package cipher

import (
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewCipherCmd creates the cipher command that integrates with SOPS.
func NewCipherCmd(_ *di.Runtime) *cobra.Command {
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
		Annotations: map[string]string{
			// Consolidate cipher subcommands (encrypt, decrypt, edit, import)
			// into tools split by permission: cipher_read and cipher_write.
			// The "cipher_operation" parameter will select which operation to perform.
			annotations.AnnotationConsolidate: "cipher_operation",
		},
	}

	// Add subcommands
	cmd.AddCommand(NewEncryptCmd())
	cmd.AddCommand(NewEditCmd())
	cmd.AddCommand(NewDecryptCmd())
	cmd.AddCommand(NewImportCmd())

	return cmd
}
