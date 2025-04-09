using System.ComponentModel;
using System.Diagnostics;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Schema;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;

namespace KSail.Docs.Tests.Unit;


public class DeclarativeConfigGeneratorTests
{
  [Fact]
  public async Task GenerateDeclarativeConfig_ShouldReturnDeclarativeConfigCodeSnippet()
  {
    // Arrange & Act
    string expectedCodeSnippet = DeclarativeConfigGenerator.Generate();
    expectedCodeSnippet = expectedCodeSnippet
      .Replace("\\", "/", StringComparison.Ordinal)
      .Replace("C:/", "/", StringComparison.Ordinal);
    string declarativeConfigMarkdownFilePath = "../../../../../../docs/configuration/declarative-config.md";
    string declarativeConfigMarkdownFileContents = await File.ReadAllTextAsync(declarativeConfigMarkdownFilePath);
    string actualCodeSnippet = RegexHelpers.YamlCodeBlockRegex().Match(declarativeConfigMarkdownFileContents).Value;

    // Assert
    _ = await Verify(expectedCodeSnippet.ToString(), extension: "md").UseFileName("declarative-config");
    Assert.Equal(expectedCodeSnippet, actualCodeSnippet);
  }
}

