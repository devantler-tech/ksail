using System.Collections.ObjectModel;
using System.CommandLine;
using System.CommandLine.IO;
using System.Text.RegularExpressions;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail.Tests.Commands.Init;

public partial class KSailInitCommandTests
{
  readonly TestConsole _console;
  readonly KSailRootCommand _ksailCommand;

  public KSailInitCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new KSailRootCommand(_console);
  }

  [Fact]
  public async Task KSailInitHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(["init", "--help"], _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out);
  }


  [SkippableTheory]
  [InlineData(["init", "--output", "ksail-init-default"])]
  [InlineData(["init", "--output", "ksail-init-docker", "--provider", "Docker"])]
  [InlineData(["init", "--output", "ksail-init-docker-none", "--provider", "Docker", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-docker-native", "--provider", "Docker", "--distribution", "Native"])]
  [InlineData(["init", "--output", "ksail-init-docker-native-kubectl", "--provider", "Docker", "--distribution", "Native", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-docker-native-kubectl-cilium", "--provider", "Docker", "--distribution", "Native", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-native-kubectl-cilium-sops", "--provider", "Docker", "--distribution", "Native", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-native-flux", "--provider", "Docker", "--distribution", "Native", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-docker-native-flux-cilium", "--provider", "Docker", "--distribution", "Native", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-native-flux-cilium-sops", "--provider", "Docker", "--distribution", "Native", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s", "--provider", "Docker", "--distribution", "K3s"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-none", "--provider", "Docker", "--distribution", "K3s", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-kubectl", "--provider", "Docker", "--distribution", "K3s", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-kubectl-cilium", "--provider", "Docker", "--distribution", "K3s", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-kubectl-cilium-sops", "--provider", "Docker", "--distribution", "K3s", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-flux", "--provider", "Docker", "--distribution", "K3s", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-flux-cilium", "--provider", "Docker", "--distribution", "K3s", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3s-flux-cilium-sops", "--provider", "Docker", "--distribution", "K3s", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman", "--provider", "Podman"])]
  [InlineData(["init", "--output", "ksail-init-podman-none", "--provider", "Podman", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-podman-native", "--provider", "Podman", "--distribution", "Native"])]
  [InlineData(["init", "--output", "ksail-init-podman-native-kubectl", "--provider", "Podman", "--distribution", "Native", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-podman-native-kubectl-cilium", "--provider", "Podman", "--distribution", "Native", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-native-kubectl-cilium-sops", "--provider", "Podman", "--distribution", "Native", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman-native-flux", "--provider", "Podman", "--distribution", "Native", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-podman-native-flux-cilium", "--provider", "Podman", "--distribution", "Native", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-native-flux-cilium-sops", "--provider", "Podman", "--distribution", "Native", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s", "--provider", "Podman", "--distribution", "K3s"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-none", "--provider", "Podman", "--distribution", "K3s", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-kubectl", "--provider", "Podman", "--distribution", "K3s", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-kubectl-cilium", "--provider", "Podman", "--distribution", "K3s", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-kubectl-cilium-sops", "--provider", "Podman", "--distribution", "K3s", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-flux", "--provider", "Podman", "--distribution", "K3s", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-flux-cilium", "--provider", "Podman", "--distribution", "K3s", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3s-flux-cilium-sops", "--provider", "Podman", "--distribution", "K3s", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  public async Task KSailInit_WithVariousOptions_SucceedsAndGeneratesKSailProject(params string[] args)
  {
    //TODO: Add support for Windows at a later time.
    Skip.If(OperatingSystem.IsWindows(), "Skipping test on Windows OS.");
    //Arrange
    if (args == null)
    {
      throw new ArgumentNullException(nameof(args), "The argument 'args' cannot be null.");
    }
    string outputDir = args[2];

    //Act
    int exitCode = await _ksailCommand.InvokeAsync(args).ConfigureAwait(false);

    //Assert
    Assert.Equal(0, exitCode);
    Assert.True(Directory.Exists(outputDir), $"Directory {outputDir} does not exist.");
    foreach (string file in Directory.GetFiles(outputDir, "*", SearchOption.AllDirectories))
    {
      string fileName = Path.GetFileName(file);
      if (fileName == ".sops.yaml")
      {
        continue;
      }
      string relativefilePath = file.Replace(outputDir, "", StringComparison.OrdinalIgnoreCase).TrimStart(Path.DirectorySeparatorChar);
      relativefilePath = relativefilePath.Replace(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
      string? directoryPath = Path.GetDirectoryName(relativefilePath);
      _ = await Verify(await File.ReadAllTextAsync(file).ConfigureAwait(false), extension: "yaml")
          .UseDirectory(Path.Combine(outputDir, directoryPath!))
          .UseFileName(fileName)
          .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }


    //Cleanup
    var secretsManager = new SOPSLocalAgeSecretManager();
    if (File.Exists(Path.Combine(outputDir, ".sops.yaml")))
    {
      var sopsConfig = await SopsConfigLoader.LoadAsync(outputDir).ConfigureAwait(false);
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
    Directory.Delete(outputDir, true);
  }

  [GeneratedRegex("url:.*")]
  private static partial Regex UrlRegex();
}
