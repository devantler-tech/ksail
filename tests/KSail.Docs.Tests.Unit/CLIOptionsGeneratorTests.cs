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
    expectedMarkdown = expectedMarkdown.Replace("testhost", "ksail", StringComparison.Ordinal);

    // Assert
    _ = await Verify(expectedMarkdown.ToString(), extension: "md").UseFileName("cli-options");
  }
}

