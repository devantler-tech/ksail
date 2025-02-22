using System.CommandLine;
using KSail.Commands.Secrets.Commands;
using KSail.Options;

namespace KSail.Commands.Secrets;

sealed class KSailSecretsCommand : Command
{
  internal KSailSecretsCommand(GlobalOptions globalOptions, IConsole? console = default) : base("secrets", "Manage secrets")
  {
    AddCommands(globalOptions);
    this.SetHandler(async (context) =>
      {
        context.ExitCode = await this.InvokeAsync("--help", console).ConfigureAwait(false);
      }
    );
  }

  void AddCommands(GlobalOptions globalOptions)
  {
    AddCommand(new KSailSecretsEncryptCommand(globalOptions));
    AddCommand(new KSailSecretsDecryptCommand(globalOptions));
    // TODO: Include `ksail secrets edit` command when pseudo-terminal support is added to CLIWrap. See https://github.com/Tyrrrz/CliWrap/issues/225.
    //AddCommand(new KSailSecretsEditCommand());
    AddCommand(new KSailSecretsAddCommand(globalOptions));
    AddCommand(new KSailSecretsRemoveCommand(globalOptions));
    AddCommand(new KSailSecretsListCommand(globalOptions));
    AddCommand(new KSailSecretsImportCommand(globalOptions));
    AddCommand(new KSailSecretsExportCommand(globalOptions));
  }
}
