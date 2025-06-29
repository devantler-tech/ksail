using System.CommandLine;
using KSail.Commands.Secrets.Commands;
using KSail.Options;

namespace KSail.Commands.Secrets;

sealed class KSailSecretsCommand : Command
{
  internal KSailSecretsCommand() : base("secrets", "Manage secrets") => AddCommands();

  void AddCommands()
  {
    Subcommands.Add(new KSailSecretsEncryptCommand());
    Subcommands.Add(new KSailSecretsDecryptCommand());
    Subcommands.Add(new KSailSecretsEditCommand());
    Subcommands.Add(new KSailSecretsAddCommand());
    Subcommands.Add(new KSailSecretsRemoveCommand());
    Subcommands.Add(new KSailSecretsListCommand());
    Subcommands.Add(new KSailSecretsImportCommand());
    Subcommands.Add(new KSailSecretsExportCommand());
  }
}
