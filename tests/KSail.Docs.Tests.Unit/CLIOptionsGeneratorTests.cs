using System.ComponentModel;
using System.Diagnostics;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Schema;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;

namespace KSail.Docs.Tests.Unit;


public class CLIOptionsGeneratorTests
{
  [Fact]
  public async Task GenerateCLIOptions_ShouldReturnCLIOptionsMarkdown()
  {
    // Arrange & Act
    string expectedMarkdown = await CLIOptionsGenerator.GenerateAsync();
    expectedMarkdown = expectedMarkdown
      .Replace("testhost", "ksail", StringComparison.Ordinal)
      .Replace("\\", "/", StringComparison.Ordinal)
      .Replace("C:/", "/", StringComparison.Ordinal);
    string actualMarkdown = await File.ReadAllTextAsync("../../../../../../docs/configuration/cli-options.md");

    // Assert
    _ = await Verify(expectedMarkdown.ToString(), extension: "md").UseFileName("cli-options");
    Assert.Equal(expectedMarkdown, actualMarkdown);
  }
}

