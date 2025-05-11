
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Flux;

class KSailGenFluxCommand : Command
{
  public KSailGenFluxCommand() : base("flux", "Generate a Flux resource.") => AddCommands();

  void AddCommands()
  {
    AddCommand(new KSailGenFluxHelmReleaseCommand());
    AddCommand(new KSailGenFluxHelmRepositoryCommand());
    AddCommand(new KSailGenFluxKustomizationCommand());
  }
}
