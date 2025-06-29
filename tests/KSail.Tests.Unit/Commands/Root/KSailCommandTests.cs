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
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int exitCode = await _ksailCommand.Parse([], commandLineConfiguration).InvokeAsync(cts.Token);

    //Assert
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString());
    Assert.Equal(0, exitCode);
  }


  [Fact]
  public async Task KSailHelp_SucceedsAndPrintsHelp()
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
    int exitCode = await _ksailCommand.Parse(["--help"], commandLineConfiguration).InvokeAsync(cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString());
  }
}
