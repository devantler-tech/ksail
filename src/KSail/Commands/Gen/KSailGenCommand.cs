
using System.CommandLine;
using KSail.Commands.Gen.Commands.CertManager;
using KSail.Commands.Gen.Commands.Config;
using KSail.Commands.Gen.Commands.Flux;
using KSail.Commands.Gen.Commands.Kustomize;
using KSail.Commands.Gen.Commands.Native;
using KSail.Models;
using KSail.Options.Generator;

namespace KSail.Commands.Gen;

sealed class KSailGenCommand : Command
{
  readonly GeneratorOverwriteOption _generatorOverwriteOption = new(new KSailCluster());
  internal KSailGenCommand(IConsole? console = default) : base("gen", "Generate a resource")
  {
    AddGlobalOption(_generatorOverwriteOption);
    AddCommands(console);
    this.SetHandler(async (context) =>
      {
        context.ExitCode = await this.InvokeAsync("--help", console).ConfigureAwait(false);
      }
    );
  }

  void AddCommands(IConsole? console)
  {
    AddCommand(new KSailGenCertManagerCommand(console));
    AddCommand(new KSailGenConfigCommand(console));
    AddCommand(new KSailGenFluxCommand(console));
    AddCommand(new KSailGenKustomizeCommand(console));
    AddCommand(new KSailGenNativeCommand(console));
  }
}


