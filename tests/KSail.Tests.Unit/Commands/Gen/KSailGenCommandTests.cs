using System.CommandLine;
using System.CommandLine.Parsing;
using System.Diagnostics.CodeAnalysis;
using System.Text.RegularExpressions;
using KSail.Commands.Root;
using KSail.Tests.Unit.Commands.Gen;

namespace KSail.Tests.Commands.Gen;

public partial class KSailGenCommandTests
{
  readonly Command _ksailCommand;

  public KSailGenCommandTests() => _ksailCommand = new KSailRootCommand();

  [Theory]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.HelpTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  public async Task KSailGen_SucceedsAndPrintsHelp(string[] command)
  {
    //Act
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int exitCode = await _ksailCommand.Parse(command, commandLineConfiguration).InvokeAsync(cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString()).UseFileName($"ksail {string.Join(" ", command)}");
  }


  /// <returns></returns>
  [Theory]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateCertManagerResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateConfigResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateFluxResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateKustomizeResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.GenerateNativeResourceTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  public async Task KSailGen_SucceedsAndGeneratesAResource(string[] args, string fileName)
  {
    //Act
    string outputPath = Path.Combine(Path.GetTempPath(), fileName);
    if (File.Exists(outputPath))
    {
      File.Delete(outputPath);
    }
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int exitCode = await _ksailCommand.Parse([.. args, "--output", outputPath], commandLineConfiguration).InvokeAsync(cts.Token);
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
