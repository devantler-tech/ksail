using System.CommandLine;
using System.CommandLine.Builder;
using System.CommandLine.IO;
using System.CommandLine.Parsing;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.List;


public class KSailListCommandTests
{
  [Fact]
  public async Task KSailListHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new CommandLineBuilder(new KSailRootCommand(new SystemConsole()))
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

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["list", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
