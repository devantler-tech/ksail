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
    AddCommand(new KSailRunAgeKeygenCommand());
    AddCommand(new KSailRunCiliumCommand());
    AddCommand(new KSailRunFluxCommand());
    AddCommand(new KSailRunHelmCommand());
    AddCommand(new KSailRunK3dCommand());
    AddCommand(new KSailRunK9sCommand());
    AddCommand(new KSailRunKindCommand());
    AddCommand(new KSailRunKubeconformCommand());
    AddCommand(new KSailRunKubectlCommand());
    AddCommand(new KSailRunKustomizeCommand());
    AddCommand(new KSailRunSopsCommand());
  }
}
