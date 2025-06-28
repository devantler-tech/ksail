using System.CommandLine;

namespace KSail.Options;


class GenericPathOption : Option<string?>
{
  public GenericPathOption(string name, string[] aliases, string defaultPath)
    : base(name, aliases)
  {
    Description = "A file or directory path.";
    DefaultValueFactory = _ => defaultPath ?? string.Empty;
  }
}
