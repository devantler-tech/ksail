using System.CommandLine;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Stop;


public class KSailStopCommandTests
{
  [Fact]
  public async Task KSailStopHelp_SucceedsAndPrintsIntroductionAndHelp()
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
    int exitCode = await ksailCommand.Parse(["stop", "-h"]).InvokeAsync(invocationConfiguration, cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString());
  }
}
