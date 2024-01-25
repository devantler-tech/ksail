using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Down;
using KSail.Tests.Integration.TestUtils;

namespace KSail.Tests.Integration.Commands.Down;

/// <summary>
/// Tests for the <see cref="KSailDownCommand"/> class.
/// </summary>
[Collection("KSail Tests Collection")]
public class KSailDownCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => KSailTestUtils.CleanupAsync();

  /// <summary>
  /// Tests that the <c>ksail down</c> command fails and prints help.
  /// </summary>
  [Fact]
  public async Task KSailDown_FailsAndPrintsHelp()
  {
    Console.WriteLine($"🧪 Running {nameof(KSailDown_FailsAndPrintsHelp)} test...");
    //Arrange
    var console = new TestConsole();
    var ksailDownCommand = new KSailDownCommand();

    //Act
    int exitCode = await ksailDownCommand.InvokeAsync("", console);

    //Assert
    Assert.Equal(1, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
