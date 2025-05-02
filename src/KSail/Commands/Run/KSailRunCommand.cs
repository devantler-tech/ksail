using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Run.Commands;
using KSail.Commands.Secrets.Commands;
using KSail.Options;

namespace KSail.Commands.Run;

sealed class KSailRunCommand : Command
{
  internal KSailRunCommand() : base("run", "Run a command") => AddCommands();
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
