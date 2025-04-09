using System.Text.RegularExpressions;
using KSail.Generator.Docs;

string cliOptionsMarkdown = await CLIOptionsGenerator.Generate().ConfigureAwait(false);
await File.WriteAllTextAsync("../../docs/configuration/cli-options.md", cliOptionsMarkdown).ConfigureAwait(false);

string declarativeConfigMarkdown = DeclarativeConfigGenerator.Generate();
string declarativeConfigMarkdownFilePath = "../../docs/configuration/declarative-config.md";
string declarativeConfigMarkdownFileContents = await File.ReadAllTextAsync(declarativeConfigMarkdownFilePath).ConfigureAwait(false);
string declarativeConfigMarkdownFileContentsNew = RegexHelpers.YamlCodeBlockRegex().Replace(declarativeConfigMarkdownFileContents, declarativeConfigMarkdown);
await File.WriteAllTextAsync("../../docs/configuration/declarative-config.md", declarativeConfigMarkdownFileContentsNew).ConfigureAwait(false);


static partial class RegexHelpers
{
  [GeneratedRegex(@"```yaml[\s\S]+```", RegexOptions.Multiline)]
  public static partial Regex YamlCodeBlockRegex();
}
