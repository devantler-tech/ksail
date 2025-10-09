using System.CommandLine;
using System.CommandLine.Parsing;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Connect;


public class KSailConnectCommandTests
{
  [Fact]
  public async Task KSailDebugHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var ksailCommand = new KSailRootCommand();

    //Act
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var invocationConfiguration = new InvocationConfiguration()
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int exitCode = await ksailCommand.Parse(["connect", "--help"]).InvokeAsync(invocationConfiguration, cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString());
  }
}
