using System.CommandLine;
using System.CommandLine.Builder;
using System.CommandLine.IO;
using System.CommandLine.Parsing;
using System.Diagnostics.CodeAnalysis;
using System.Text.RegularExpressions;
using KSail.Commands.Root;
using KSail.Tests.Unit.Commands.Gen;

namespace KSail.Tests.Commands.Gen;

public partial class KSailGenCommandTests
{
  readonly TestConsole _console;
  readonly Parser _ksailCommand;

  public KSailGenCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new CommandLineBuilder(new KSailRootCommand(_console))
      .UseVersionOption()
      .UseHelp("--help")
      .UseEnvironmentVariableDirective()
      .UseParseDirective()
      .UseSuggestDirective()
      .RegisterWithDotnetSuggest()
      .UseTypoCorrections()
      .UseParseErrorReporting()
      .UseExceptionHandler()
      .CancelOnProcessTermination()
      .Build();
  }

  [Theory]
  [MemberData(nameof(KSailGenCommandTestsTheoryData.HelpTheoryData), MemberType = typeof(KSailGenCommandTestsTheoryData))]
  public async Task KSailGen_SucceedsAndPrintsHelp(string[] command)
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(command, _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out).UseFileName($"ksail {string.Join(" ", command)}");
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
    int exitCode = await _ksailCommand.InvokeAsync([.. args, "--output", outputPath], _console);
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
