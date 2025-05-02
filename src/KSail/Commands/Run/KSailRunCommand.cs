using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Run.Commands;
using KSail.Commands.Secrets.Commands;
using KSail.Options;

namespace KSail.Commands.Run;

sealed class KSailRunCommand : Command
{
  internal KSailRunCommand(IConsole? console = default) : base("run", "Run a command")
  {
    console ??= new SystemConsole();
    AddCommands();
    this.SetHandler(async (context) =>
      {
        context.ExitCode = await this.InvokeAsync("--help", console).ConfigureAwait(false);
      }
    );
  }
  void AddCommands()
  {
    AddCommand(new KSailRunKindCommand());
    AddCommand(new KSailRunK3dCommand());
    AddCommand(new KSailRunKubectlCommand());
    AddCommand(new KSailRunFluxCommand());
    AddCommand(new KSailRunHelmCommand());
    AddCommand(new KSailRunCiliumCommand());
    AddCommand(new KSailRunKustomizeCommand());
    AddCommand(new KSailRunKubeconformCommand());
    AddCommand(new KSailRunSopsCommand());
    AddCommand(new KSailRunAgeKeygenCommand());
    AddCommand(new KSailRunK9sCommand());
  }
}
