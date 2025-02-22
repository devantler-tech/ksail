using System.CommandLine;

namespace KSail.Options;

/// <summary>
/// A path.
/// </summary>
/// <param name="description"></param>
/// <param name="aliases"></param>
public class PathOption(string description, string[]? aliases = default) : Option<string?>(
    aliases ?? ["--path", "-p"],
    description
  )
{
}
