
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Flux;

class KSailGenFluxCommand : Command
{
  public KSailGenFluxCommand() : base("flux", "Generate a Flux resource.") => AddCommands();

  void AddCommands()
  {
    Subcommands.Add(new KSailGenFluxHelmReleaseCommand());
    Subcommands.Add(new KSailGenFluxHelmRepositoryCommand());
    Subcommands.Add(new KSailGenFluxKustomizationCommand());
  }
}
