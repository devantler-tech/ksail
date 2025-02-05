using System.CommandLine;
using KSail.Commands.Down;
using KSail.Commands.Init;
using KSail.Commands.List;
using KSail.Commands.Start;
using KSail.Commands.Stop;
using KSail.Commands.Up;
using KSail.Commands.Update;

namespace KSail.Tests.E2E;

/// <summary>
/// E2E tests for the various distributions.
/// </summary>
[Collection("KSail.Tests")]
public class E2ETests : IAsyncLifetime, IDisposable
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Cleanup the test environment.
  /// </summary>
  /// <returns></returns>
  static void Cleanup()
  {
    if (Directory.Exists("k8s"))
      Directory.Delete("k8s", true);
    if (File.Exists("kind-config.yaml"))
      File.Delete("kind-config.yaml");
    if (File.Exists("k3d-config.yaml"))
      File.Delete("k3d-config.yaml");
    if (File.Exists("ksail-config.yaml"))
      File.Delete("ksail-config.yaml");
    if (File.Exists(".sops.yaml"))
      File.Delete(".sops.yaml");
  }

  /// <summary>
  /// Tests that the 'ksail up' command is executed successfully with various configurations.
  /// </summary>
  [Theory]
  [InlineData("-d native")]
  //TODO: Add back --secret-manager sops
  [InlineData("--name ksail-advanced-native --distribution native --flux-post-build-variables")]
  [InlineData("-d k3s")]
  //TODO: Add back --secret-manager sops
  [InlineData("--name ksail-advanced-k3s --distribution k3s --flux-post-build-variables")]
  public async Task KSailUp_WithVariousConfigurations_Succeeds(string initArgs)
  {
    //Arrange
    var ksailInitCommand = new KSailInitCommand();
    var ksailUpCommand = new KSailUpCommand();
    var ksailListCommand = new KSailListCommand();
    var ksailStopCommand = new KSailStopCommand();
    var ksailStartCommand = new KSailStartCommand();
    var ksailUpdateCommand = new KSailUpdateCommand();
    var ksailDownCommand = new KSailDownCommand();

    //Act & Assert
    int initExitCode = await ksailInitCommand.InvokeAsync(initArgs);
    Assert.Equal(0, initExitCode);
    int upExitCode = await ksailUpCommand.InvokeAsync("--destroy");
    Assert.Equal(0, upExitCode);
    int listExitCode = await ksailListCommand.InvokeAsync("");
    Assert.Equal(0, listExitCode);
    int stopExitCode = await ksailStopCommand.InvokeAsync("");
    Assert.Equal(0, stopExitCode);
    int startExitCode = await ksailStartCommand.InvokeAsync("");
    Assert.Equal(0, startExitCode);
    int updateExitCode = await ksailUpdateCommand.InvokeAsync("");
    Assert.Equal(0, updateExitCode);
    int downExitCode = await ksailDownCommand.InvokeAsync("--registries");
    Assert.Equal(0, downExitCode);
  }

  /// <inheritdoc/>
  public void Dispose()
  {
    Cleanup();
    GC.SuppressFinalize(this);
  }
}
