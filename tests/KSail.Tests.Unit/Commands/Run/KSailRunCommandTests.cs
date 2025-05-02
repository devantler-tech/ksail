using System.CommandLine;
using System.CommandLine.IO;
using System.Diagnostics.CodeAnalysis;
using System.Text.RegularExpressions;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Run;

public partial class KSailRunCommandTests
{
  readonly TestConsole _console;
  readonly KSailRootCommand _ksailCommand;

  public KSailRunCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new KSailRootCommand(_console);
  }

  [Theory]
  [MemberData(nameof(KSailRunCommandTestsTheoryData.HelpTheoryData), MemberType = typeof(KSailRunCommandTestsTheoryData))]
  public async Task KSailRun_SucceedsAndPrintsHelp(string[] command)
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(command, _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out).UseFileName($"ksail {string.Join(" ", command)}");
  }
}
