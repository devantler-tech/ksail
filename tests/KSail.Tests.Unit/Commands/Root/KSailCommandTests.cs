using System.CommandLine;
using System.CommandLine.Builder;
using System.CommandLine.IO;
using System.CommandLine.Parsing;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Root;

public class KSailRootCommandTests
{
  readonly TestConsole _console;
  readonly Command _ksailCommand;

  public KSailRootCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new KSailRootCommand(_console);
  }

  [Fact]
  public async Task KSail_SucceedsAndPrintsIntroduction()
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync([]);

    //Assert
    _ = await Verify(_console.Error.ToString() + _console.Out);
    Assert.Equal(0, exitCode);
  }


  [Fact]
  public async Task KSailHelp_SucceedsAndPrintsHelp()
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(["--help"], _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out);
  }
}
