using System.CommandLine;
using System.CommandLine.Parsing;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Root;

public class KSailRootCommandTests
{
  readonly Command _ksailCommand;

  public KSailRootCommandTests() => _ksailCommand = new KSailRootCommand();

  [Fact]
  public async Task KSail_SucceedsAndPrintsIntroduction()
  {
    //Act
    int exitCode = await parseResult.InvokeAsync([]);

    //Assert
    _ = await Verify(_console.Error.ToString() + _console.Out);
    Assert.Equal(0, exitCode);
  }


  [Fact]
  public async Task KSailHelp_SucceedsAndPrintsHelp()
  {
    //Act
    int exitCode = await parseResult.InvokeAsync(["--help"]);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out);
  }
}
