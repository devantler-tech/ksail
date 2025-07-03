
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Config;

class KSailGenConfigCommand : Command
{
  public KSailGenConfigCommand() : base("config", "Generate a configuration file.") => AddCommands();

  void AddCommands()
  {
    Subcommands.Add(new KSailGenConfigK3dCommand());
    Subcommands.Add(new KSailGenConfigKSailCommand());
    Subcommands.Add(new KSailGenConfigSOPSCommand());
  }
}


