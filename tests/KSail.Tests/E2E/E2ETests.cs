using System.CommandLine;
using System.CommandLine.IO;
using System.Runtime.InteropServices;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Root;
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
    var ksailCommand = new KSailRootCommand(console);

    //Act & Assert
    int initExitCode = await ksailCommand.InvokeAsync(initArgs);
    Assert.Equal(0, initExitCode);
    int upExitCode = await ksailCommand.InvokeAsync(["up"], console);
    Assert.Equal(0, upExitCode);
    int listExitCode = await ksailCommand.InvokeAsync(["list"], console);
    Assert.Equal(0, listExitCode);
    int stopExitCode = await ksailCommand.InvokeAsync(["stop"], console);
    Assert.Equal(0, stopExitCode);
    int startExitCode = await ksailCommand.InvokeAsync(["start"], console);
    Assert.Equal(0, startExitCode);
    int updateExitCode = await ksailCommand.InvokeAsync(["update"], console);
    Assert.Equal(0, updateExitCode);
    int downExitCode = await ksailCommand.InvokeAsync(["down"], console);
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
    if (File.Exists("kind-config.yaml"))
      File.Delete("kind-config.yaml");
    if (File.Exists("k3d-config.yaml"))
      File.Delete("k3d-config.yaml");
    if (File.Exists("ksail-config.yaml"))
      File.Delete("ksail-config.yaml");
    if (File.Exists(".sops.yaml"))
      File.Delete(".sops.yaml");
  }
}
