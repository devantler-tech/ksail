using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Start;


public class KSailStartCommandTests
{
  [Fact]
  public async Task KSailStartHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["start", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
