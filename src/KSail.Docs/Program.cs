#pragma warning disable CA1515 // Consider making public types internal
#pragma warning disable IDE0060 // Remove unused parameter
using System.Diagnostics.CodeAnalysis;

namespace KSail.Docs;

[ExcludeFromCodeCoverage]
public static class Program
{
  public static async Task Main(string[] args)
  {
    // Generate CLIOptions documentation
    string cliOptionsMarkdown = await CLIOptionsGenerator.GenerateAsync().ConfigureAwait(false);
    await File.WriteAllTextAsync("../../docs/configuration/cli-options.md", cliOptionsMarkdown).ConfigureAwait(false);

    // Generate declarative config code snippet
    string declarativeConfigMarkdown = DeclarativeConfigGenerator.Generate();
    string declarativeConfigMarkdownFilePath = "../../docs/configuration/declarative-config.md";
    string declarativeConfigMarkdownFileContents = await File.ReadAllTextAsync(declarativeConfigMarkdownFilePath).ConfigureAwait(false);
    string declarativeConfigMarkdownFileContentsNew = RegexHelpers.YamlCodeBlockRegex().Replace(declarativeConfigMarkdownFileContents, declarativeConfigMarkdown);
    await File.WriteAllTextAsync("../../docs/configuration/declarative-config.md", declarativeConfigMarkdownFileContentsNew).ConfigureAwait(false);

    // Generate JSON Schema
    string jsonSchema = SchemaGenerator.Generate();
    await File.WriteAllTextAsync("../../schemas/ksail-cluster-schema.json", jsonSchema).ConfigureAwait(false);
  }
}
#pragma warning restore CA1515 // Consider making public types internal
#pragma warning restore IDE0060 // Remove unused parameter
