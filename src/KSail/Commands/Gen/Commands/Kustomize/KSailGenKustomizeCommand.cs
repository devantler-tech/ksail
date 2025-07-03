
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Kustomize;

class KSailGenKustomizeCommand : Command
{
  public KSailGenKustomizeCommand() : base("kustomize", "Generate a Kustomize resource.") => AddCommands();

  void AddCommands()
  {
    Subcommands.Add(new KSailGenKustomizeComponentCommand());
    Subcommands.Add(new KSailGenKustomizeKustomizationCommand());
  }
}
