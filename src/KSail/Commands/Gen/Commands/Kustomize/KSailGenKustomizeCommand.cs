
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Kustomize;

class KSailGenKustomizeCommand : Command
{
  public KSailGenKustomizeCommand() : base("kustomize", "Generate a Kustomize resource.") => AddCommands();

  void AddCommands()
  {
    AddCommand(new KSailGenKustomizeComponentCommand());
    AddCommand(new KSailGenKustomizeKustomizationCommand());
  }
}
