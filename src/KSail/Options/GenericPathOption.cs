using System.CommandLine;

namespace KSail.Options;


class GenericPathOption(string? defaultPath = default, string[]? aliases = default) : Option<string?>(
  aliases ?? ["-o", "--output"],
  () => defaultPath,
  "A file or directory path."
)
{ }
