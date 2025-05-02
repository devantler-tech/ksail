
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Config;

class KSailGenConfigCommand : Command
{
  public KSailGenConfigCommand() : base("config", "Generate a configuration file.") => AddCommands();

  void AddCommands()
  {
    AddCommand(new KSailGenConfigK3dCommand());
    AddCommand(new KSailGenConfigKSailCommand());
    AddCommand(new KSailGenConfigSOPSCommand());
  }
}


