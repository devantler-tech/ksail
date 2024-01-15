using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands;

namespace KSail.Tests.Integration.Commands;

/// <summary>
/// Tests for the <see cref="KSailCommand"/> class.
/// </summary>
[UsesVerify]
public class KSailCommandTests
{
  /// <summary>
  /// Tests that the 'ksail' command succeeds and returns the introduction and help text.
  /// </summary>
  [Fact]
  public async void NoOptions_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync("", console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Out.ToString());
  }

  /// <summary>
  /// Tests that the 'ksail --help' command succeeds and returns the help text.
  /// </summary>
  [Fact]
  public async void HelpOption_SucceedsAndPrintsHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync("--help", console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
