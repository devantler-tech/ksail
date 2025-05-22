using System.Text.RegularExpressions;

namespace KSail.Docs;

static partial class RegexHelpers
{
  [GeneratedRegex(@"```yaml[\s\S]+```", RegexOptions.Multiline)]
  public static partial Regex YamlCodeBlockRegex();
}
