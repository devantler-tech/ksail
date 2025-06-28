using System.ComponentModel;
using System.Diagnostics;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Schema;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;
using System.Text.RegularExpressions;

namespace KSail.Docs.Tests.Unit;


public partial class CLIOptionsGeneratorTests
{
  [Fact]
  public async Task GenerateCLIOptions_ShouldReturnCLIOptionsMarkdown()
  {
    // Arrange & Act
    string expectedMarkdown = await CLIOptionsGenerator.GenerateAsync();
    expectedMarkdown = expectedMarkdown
      .Replace("testhost", "ksail", StringComparison.Ordinal)
      .Replace("\\", "/", StringComparison.Ordinal);
    string actualMarkdown = await File.ReadAllTextAsync("../../../../../../docs/configuration/cli-options.md");

    // Assert
    _ = await Verify(expectedMarkdown.ToString(), extension: "md").UseFileName("cli-options");
    Assert.Equal(
      ReplaceWhitespace(Normalize(expectedMarkdown), ""),
      ReplaceWhitespace(Normalize(actualMarkdown), "")
    );
  }

  static readonly Regex _sWhitespace = WhiteSpaceRegex();
  public static string ReplaceWhitespace(string input, string replacement) => _sWhitespace.Replace(input, replacement);

  static string Normalize(string s) => s.Replace("\r", "", StringComparison.Ordinal).Replace("\n", "", StringComparison.Ordinal);
  [GeneratedRegex(@"\s+")]
  private static partial Regex WhiteSpaceRegex();
}

