using System.CommandLine;
using KSail.Commands.Secrets.Commands;

namespace KSail.Commands.Secrets;

sealed class KSailSecretsCommand : Command
{
  internal KSailSecretsCommand(IConsole? console = default) : base("secrets", "Manage secrets")
  {
    AddCommands();
    this.SetHandler(async (context) =>
      {
        context.ExitCode = await this.InvokeAsync("--help", console).ConfigureAwait(false);
      }
    );
  }

  void AddCommands()
  {
    AddCommand(new KSailSecretsEncryptCommand());
    AddCommand(new KSailSecretsDecryptCommand());
    AddCommand(new KSailSecretsGenerateCommand());
    AddCommand(new KSailSecretsDeleteCommand());
    AddCommand(new KSailSecretsListCommand());
    AddCommand(new KSailSecretsImportCommand());
    AddCommand(new KSailSecretsExportCommand());
  }
}
