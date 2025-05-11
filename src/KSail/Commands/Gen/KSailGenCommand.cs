
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
  internal KSailGenCommand() : base("gen", "Generate a resource")
  {
    AddGlobalOption(_generatorOverwriteOption);
    AddCommands();
  }

  void AddCommands()
  {
    AddCommand(new KSailGenCertManagerCommand());
    AddCommand(new KSailGenConfigCommand());
    AddCommand(new KSailGenFluxCommand());
    AddCommand(new KSailGenKustomizeCommand());
    AddCommand(new KSailGenNativeCommand());
  }
}


