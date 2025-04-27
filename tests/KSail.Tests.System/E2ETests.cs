using System;
using System.CommandLine;
using System.CommandLine.IO;
using System.Runtime.InteropServices;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Connect;
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
  // Docker + Kind + Defaults
  [InlineData(["init", "--name", "d-n-defaults", "--container-engine", "Docker", "--distribution", "Kind"])]
  // Docker + Kind + Cilium CNI
  [InlineData(["init", "--name", "d-n-cilium", "--container-engine", "Docker", "--distribution", "Kind", "--cni", "Cilium"])]
  // Docker + Kind + No CNI
  [InlineData(["init", "--name", "d-n-no-cni", "--container-engine", "Docker", "--distribution", "Kind", "--cni", "None"])]
  // Docker + Kind + No CSI
  [InlineData(["init", "--name", "d-n-no-csi", "--container-engine", "Docker", "--distribution", "Kind", "--csi", "None"])]
  // Docker + Kind + Traefik Ingress Controller
  [InlineData(["init", "--name", "d-n-traefik", "--container-engine", "Docker", "--distribution", "Kind", "--ingress-controller", "Traefik"])]
  // Docker + Kind + Kubectl + SOPS
  [InlineData(["init", "--name", "d-n-k-sops", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Kubectl", "--secret-manager", "SOPS"])]
  // Docker + Kind + Flux + Defaults
  [InlineData(["init", "--name", "d-n-f-defaults", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Flux"])]
  // Docker + Kind + Flux + SOPS
  [InlineData(["init", "--name", "d-n-f-sops", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Flux", "--secret-manager", "SOPS"])]
  // Docker + K3d + Defaults
  [InlineData(["init", "--name", "d-k-defaults", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Kubectl"])]
  // Docker + K3d + Cilium CNI
  [InlineData(["init", "--name", "d-k-cilium", "--container-engine", "Docker", "--distribution", "K3d", "--cni", "Cilium"])]
  // Docker + K3d + No CNI
  [InlineData(["init", "--name", "d-k-no-cni", "--container-engine", "Docker", "--distribution", "K3d", "--cni", "None"])]
  // Docker + K3d + No CSI
  [InlineData(["init", "--name", "d-k-no-csi", "--container-engine", "Docker", "--distribution", "K3d", "--csi", "None"])]
  // Docker + K3d + No Ingress Controller
  [InlineData(["init", "--name", "d-k-no-ingress", "--container-engine", "Docker", "--distribution", "K3d", "--ingress-controller", "None"])]
  // Podman + Kind + Defaults
  [InlineData(["init", "--name", "p-n-defaults", "--container-engine", "Podman", "--distribution", "Kind"])]
  // Podman + K3d + Defaults
  [InlineData(["init", "--name", "p-k-defaults", "--container-engine", "Podman", "--distribution", "K3d"])]
  public async Task KSailUp_WithVariousConfigurations_Succeeds(params string[] initArgs)
  {
    // Validate that initArgs is not null
    ArgumentNullException.ThrowIfNull(initArgs);

    // TODO: Add support for Windows and macOS in GitHub Runners when GitHub Actions runners support dind on Windows and macOS runners.
    Skip.If(
      RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ||
      (RuntimeInformation.IsOSPlatform(OSPlatform.OSX) && Environment.GetEnvironmentVariable("GITHUB_ACTIONS") == "true"),
      "Skipping test on Windows OS or macOS in GitHub Actions.");

    string provider = initArgs.Contains("--container-engine") ? initArgs[Array.IndexOf(initArgs, "--container-engine") + 1] : string.Empty;
    string distribution = initArgs.Contains("--distribution") ? initArgs[Array.IndexOf(initArgs, "--distribution") + 1] : string.Empty;
    Skip.If(
      RuntimeInformation.IsOSPlatform(OSPlatform.OSX) && provider.Equals("Podman", StringComparison.Ordinal) && distribution.Equals("K3d", StringComparison.Ordinal),
      "Skipping test on macOS with Podman and K3d.");

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
    int listExitCode1 = await listCommand.InvokeAsync(["list"], console).ConfigureAwait(false);
    Assert.Equal(0, listExitCode1);
    int listExitCode2 = await listCommand.InvokeAsync(["list", "--all"], console).ConfigureAwait(false);
    Assert.Equal(0, listExitCode2);
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
