using System.Collections.ObjectModel;
using System.CommandLine;
using System.CommandLine.Parsing;
using System.Text.RegularExpressions;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail.Tests.Commands.Init;

public partial class KSailInitCommandTests
{
  readonly Command _ksailCommand;

  public KSailInitCommandTests() => _ksailCommand = new KSailRootCommand();

  [Fact]
  public async Task KSailInitHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Act
    int exitCode = await parseResult.InvokeAsync(["init", "--help"]);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out);
  }


  [SkippableTheory]
  [InlineData(["init", "--output", "ksail-init-default"])]
  [InlineData(["init", "--output", "ksail-init-docker", "--container-engine", "Docker"])]
  [InlineData(["init", "--output", "ksail-init-docker-none", "--container-engine", "Docker", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind", "--container-engine", "Docker", "--distribution", "Kind"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-kubectl", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-kubectl-cilium", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-kubectl-cilium-sops", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-flux", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-flux-cilium", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-flux-cilium-sops", "--container-engine", "Docker", "--distribution", "Kind", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-kind-metrics-server", "--container-engine", "Docker", "--distribution", "Kind", "--metrics-server"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d", "--container-engine", "Docker", "--distribution", "K3d"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-none", "--container-engine", "Docker", "--distribution", "K3d", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-kubectl", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-kubectl-cilium", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-kubectl-cilium-sops", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-flux", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-flux-cilium", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-flux-cilium-sops", "--container-engine", "Docker", "--distribution", "K3d", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-docker-k3d-no-metrics-server", "--container-engine", "Docker", "--distribution", "K3d", "--metrics-server", "false"])]
  [InlineData(["init", "--output", "ksail-init-podman", "--container-engine", "Podman"])]
  [InlineData(["init", "--output", "ksail-init-podman-none", "--container-engine", "Podman", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind", "--container-engine", "Podman", "--distribution", "Kind"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind-kubectl", "--container-engine", "Podman", "--distribution", "Kind", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind-kubectl-cilium", "--container-engine", "Podman", "--distribution", "Kind", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind-kubectl-cilium-sops", "--container-engine", "Podman", "--distribution", "Kind", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind-flux", "--container-engine", "Podman", "--distribution", "Kind", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind-flux-cilium", "--container-engine", "Podman", "--distribution", "Kind", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-kind-flux-cilium-sops", "--container-engine", "Podman", "--distribution", "Kind", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d", "--container-engine", "Podman", "--distribution", "K3d"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-none", "--container-engine", "Podman", "--distribution", "K3d", "--ingress-controller", "None"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-kubectl", "--container-engine", "Podman", "--distribution", "K3d", "--deployment-tool", "Kubectl"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-kubectl-cilium", "--container-engine", "Podman", "--distribution", "K3d", "--deployment-tool", "Kubectl", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-kubectl-cilium-sops", "--container-engine", "Podman", "--distribution", "K3d", "--deployment-tool", "Kubectl", "--cni", "Cilium", "--secret-manager", "SOPS"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-flux", "--container-engine", "Podman", "--distribution", "K3d", "--deployment-tool", "Flux"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-flux-cilium", "--container-engine", "Podman", "--distribution", "K3d", "--deployment-tool", "Flux", "--cni", "Cilium"])]
  [InlineData(["init", "--output", "ksail-init-podman-k3d-flux-cilium-sops", "--container-engine", "Podman", "--distribution", "K3d", "--deployment-tool", "Flux", "--cni", "Cilium", "--secret-manager", "SOPS"])]
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
    int exitCode = await parseResult.InvokeAsync(args).ConfigureAwait(false);

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
