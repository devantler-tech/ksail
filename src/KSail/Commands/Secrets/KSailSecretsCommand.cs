using System.CommandLine;
using KSail.Commands.Secrets.Commands;
using KSail.Options;

namespace KSail.Commands.Secrets;

sealed class KSailSecretsCommand : Command
{
  internal KSailSecretsCommand(IConsole? console = default) : base("secrets", "Manage secrets")
  {
    AddCommands(console);
    this.SetHandler(async (context) =>
      {
        context.ExitCode = await this.InvokeAsync("--help", console).ConfigureAwait(false);
      }
    );
  }

  void AddCommands(IConsole? console = default)
  {
    AddCommand(new KSailSecretsEncryptCommand());
    AddCommand(new KSailSecretsDecryptCommand());
    AddCommand(new KSailSecretsEditCommand());
    AddCommand(new KSailSecretsAddCommand(console));
    AddCommand(new KSailSecretsRemoveCommand());
    AddCommand(new KSailSecretsListCommand());
    AddCommand(new KSailSecretsImportCommand());
    AddCommand(new KSailSecretsExportCommand());
  }
}
