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
using KSail.Commands.Status;
using KSail.Commands.Stop;
using KSail.Commands.Up;
using KSail.Commands.Update;
using KSail.Utils;

namespace KSail.Tests.System;

public class E2ETests
{
  [SkippableTheory]
  [InlineData(["init", "-d", "native"])]
  [InlineData(["init", "--name", "ksail-native-advanced", "--distribution", "native", "--secret-manager", "--cni", "cilium"])]
  [InlineData(["init", "-d", "k3s"])]
  [InlineData(["init", "--name", "ksail-k3s-advanced", "--distribution", "k3s", "--secret-manager", "--cni", "cilium"])]
  public async Task KSailUp_WithVariousConfigurations_Succeeds(params string[] initArgs)
  {
    // TODO: Add support for Windows and macOS in GitHub Runners when GitHub Actions runners support dind on Windows and macOS runners.
    Skip.If(
      RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ||
      (RuntimeInformation.IsOSPlatform(OSPlatform.OSX) && Environment.GetEnvironmentVariable("GITHUB_ACTIONS") == "true"),
      "Skipping test on Windows OS or macOS in GitHub Actions.");

    //Arrange
    var console = new TestConsole();
    var initCommand = new KSailInitCommand();
    var upCommand = new KSailUpCommand();
    var statusCommand = new KSailStatusCommand();
    var listCommand = new KSailListCommand();
    var stopCommand = new KSailStopCommand();
    var startCommand = new KSailStartCommand();
    var updateCommand = new KSailUpdateCommand();
    var downCommand = new KSailDownCommand();

    //Act & Assert
    int initExitCode = await initCommand.InvokeAsync(initArgs).ConfigureAwait(false);
    Assert.Equal(0, initExitCode);
    int upExitCode = await upCommand.InvokeAsync(["up"], console).ConfigureAwait(false);
    Assert.Equal(0, upExitCode);
    int statusExitCode = await statusCommand.InvokeAsync(["status"], console).ConfigureAwait(false);
    Assert.Equal(0, statusExitCode);
    int listExitCode = await listCommand.InvokeAsync(["list"], console).ConfigureAwait(false);
    Assert.Equal(0, listExitCode);
    int stopExitCode = await stopCommand.InvokeAsync(["stop"], console).ConfigureAwait(false);
    Assert.Equal(0, stopExitCode);
    int startExitCode = await startCommand.InvokeAsync(["start"], console).ConfigureAwait(false);
    Assert.Equal(0, startExitCode);
    int updateExitCode = await updateCommand.InvokeAsync(["update"], console).ConfigureAwait(false);
    Assert.Equal(0, updateExitCode);
    int downExitCode = await downCommand.InvokeAsync(["down"], console).ConfigureAwait(false);
    Assert.Equal(0, downExitCode);
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
