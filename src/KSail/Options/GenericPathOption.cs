using System.CommandLine;

namespace KSail.Options;


class GenericPathOption : Option<string?>
{
  public GenericPathOption(string? defaultPath = default, string[]? aliases = default)
    : base((aliases != null && aliases.Length > 0) ? aliases[0] : "-o")
  {
    string[] additionalAliases = (aliases != null && aliases.Length > 1)
      ? aliases[1..]
      : ["--output"];

    foreach (string alias in additionalAliases)
    {
      Aliases.Add(alias);
    }

    Description = "A file or directory path.";
    DefaultValueFactory = _ => defaultPath ?? string.Empty;
  }
}
