using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Secrets.Commands;
using KSail.Options;

namespace KSail.Commands.Secrets;

sealed class KSailSecretsCommand : Command
{
  internal KSailSecretsCommand(IConsole? console = default) : base("secrets", "Manage secrets")
  {
    console ??= new SystemConsole();
    AddCommands(console);
  }

  void AddCommands(IConsole console)
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
