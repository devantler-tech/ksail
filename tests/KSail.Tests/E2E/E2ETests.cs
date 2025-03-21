using System.CommandLine;
using System.CommandLine.IO;
using System.Runtime.InteropServices;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Debug;
using KSail.Commands.Down;
using KSail.Commands.Init;
using KSail.Commands.List;
using KSail.Commands.Root;
using KSail.Commands.Start;
using KSail.Commands.Stop;
using KSail.Commands.Up;
using KSail.Commands.Update;
using KSail.Utils;

namespace KSail.Tests.E2E;


[Collection("KSail.Tests")]
public class E2ETests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;


  [Theory]
  [InlineData(["init", "-d", "native"])]
  [InlineData(["init", "--name", "ksail-advanced-native", "--distribution", "native", "--secret-manager", "sops", "--cni", "cilium"])]
  [InlineData(["init", "-d", "k3s"])]
  [InlineData(["init", "--name", "ksail-advanced-k3s", "--distribution", "k3s", "--secret-manager", "sops", "--cni", "cilium"])]
  public async Task KSailUp_WithVariousConfigurations_Succeeds(params string[] initArgs)
  {
    // TODO: Add support for Windows and macOS in GitHub Runners when GitHub Actions runners support dind on Windows and macOS runners.
    if (RuntimeInformation.IsOSPlatform(OSPlatform.Windows) || (RuntimeInformation.IsOSPlatform(OSPlatform.OSX) && Environment.GetEnvironmentVariable("GITHUB_ACTIONS") == "true"))
    {
      return;
    }

    //Arrange
    var console = new TestConsole();
    var initCommand = new KSailInitCommand();
    var upCommand = new KSailUpCommand();
    var listCommand = new KSailListCommand();
    var stopCommand = new KSailStopCommand();
    var startCommand = new KSailStartCommand();
    var updateCommand = new KSailUpdateCommand();
    var debugCommand = new KSailDebugCommand();
    var downCommand = new KSailDownCommand();

    //Act & Assert
    int initExitCode = await initCommand.InvokeAsync(initArgs);
    Assert.Equal(0, initExitCode);
    int upExitCode = await upCommand.InvokeAsync(["up"], console);
    Assert.Equal(0, upExitCode);
    int listExitCode = await listCommand.InvokeAsync(["list"], console);
    Assert.Equal(0, listExitCode);
    int stopExitCode = await stopCommand.InvokeAsync(["stop"], console);
    Assert.Equal(0, stopExitCode);
    int startExitCode = await startCommand.InvokeAsync(["start"], console);
    Assert.Equal(0, startExitCode);
    int updateExitCode = await updateCommand.InvokeAsync(["update"], console);
    Assert.Equal(0, updateExitCode);
    Task debugTask = Task.Run(async () =>
    {
      await debugCommand.InvokeAsync(["debug"], console).ConfigureAwait(false);
    });
    await debugTask.WaitAsync(TimeSpan.FromSeconds(4));
    Assert.False(debugTask.IsFaulted);
    debugTask.Dispose();
    int downExitCode = await downCommand.InvokeAsync(["down"], console);
    Assert.Equal(0, downExitCode);
  }

  /// <inheritdoc/>
  public async Task DisposeAsync()
  {
    var secretsManager = new SOPSLocalAgeSecretManager();
    if (File.Exists(".sops.yaml"))
    {
      var sopsConfig = await SopsConfigLoader.LoadAsync().ConfigureAwait(false);
      foreach (string? publicKey in sopsConfig.CreationRules.Select(rule => rule.Age))
      {
        try
        {
          _ = await secretsManager.DeleteKeyAsync(publicKey).ConfigureAwait(false);
        }
        catch (Exception)
        {
          //Ignore any exceptions
        }
      }
    }
    if (Directory.Exists("k8s"))
      Directory.Delete("k8s", true);
    if (File.Exists("kind.yaml"))
      File.Delete("kind.yaml");
    if (File.Exists("k3d.yaml"))
      File.Delete("k3d.yaml");
    if (File.Exists("ksail.yaml"))
      File.Delete("ksail.yaml");
    if (File.Exists(".sops.yaml"))
      File.Delete(".sops.yaml");
  }
}
