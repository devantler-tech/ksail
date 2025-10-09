using System.CommandLine;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Up;


public class KSailUpCommandTests
{
  [Fact]
  public async Task KSailUpHelp_SucceedsAndPrintsIntroductionAndHelp()
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
    int exitCode = await ksailCommand.Parse(["up", "-h"]).InvokeAsync(invocationConfiguration, cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString());
  }
}
