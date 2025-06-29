
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
    AddCommands();

    foreach (var subcommand in Subcommands)
    {
      AddOptionRecursively(subcommand);
    }
  }

  void AddCommands()
  {
    Subcommands.Add(new KSailGenCertManagerCommand());
    Subcommands.Add(new KSailGenConfigCommand());
    Subcommands.Add(new KSailGenFluxCommand());
    Subcommands.Add(new KSailGenKustomizeCommand());
    Subcommands.Add(new KSailGenNativeCommand());
  }
  void AddOptionRecursively(Command command)
  {
    if (command.Subcommands.Count == 0)
    {
      command.Options.Add(_generatorOverwriteOption);
    }
    else
    {
      foreach (var sub in command.Subcommands)
      {
        AddOptionRecursively(sub);
      }
    }
  }
}


