using System.CommandLine;
using System.CommandLine.IO;
using System.Diagnostics.CodeAnalysis;
using System.Text.RegularExpressions;
using KSail.Commands.Gen;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Gen;

/// <summary>
/// Tests for the <see cref="KSailGenCommand"/> class.
/// </summary>
[ExcludeFromCodeCoverage]
public partial class KSailGenCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail gen' command succeeds and returns the help text.
  /// </summary>
  [Theory]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.HelpTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  public async Task KSailGen_SucceedsAndPrintsHelp(string[] command)
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(command, console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out).UseFileName($"ksail {string.Join(" ", command)}");
  }

  /// <summary>
  /// Tests that the 'ksail gen' command generates a resource.
  /// </summary>
  /// <returns></returns>
  [Theory]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateCertManagerResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateConfigResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateFluxResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateKustomizeResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateNativeResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  public async Task KSailGen_SucceedsAndGeneratesAResource(string[] args, string fileName)
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    string outputPath = Path.Combine(Path.GetTempPath(), fileName);
    if (File.Exists(outputPath))
    {
      File.Delete(outputPath);
    }
    int exitCode = await ksailCommand.InvokeAsync([.. args, "--output", outputPath], console);
    string fileContents = await File.ReadAllTextAsync(outputPath);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(fileContents, extension: "yaml")
      .UseFileName(fileName)
      .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));

    //Cleanup
    File.Delete(outputPath);
  }

  [GeneratedRegex("url:.*")]
  private static partial Regex UrlRegex();
}
