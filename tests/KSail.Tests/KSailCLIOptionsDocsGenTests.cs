using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;

namespace KSail.Tests;

public class KSailCLIOptionsDocsGenTests
{
  [Fact]
  public async Task GenerateKSailCLIOptionsDocs_ShouldReturnMarkdown()
  {
    // Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    var helpTexts = new Dictionary<string, string?>
    {
      { "ksail", await GetHelpTextAsync(ksailCommand, "--help") },
      { "ksail up", await GetHelpTextAsync(ksailCommand, "up", "--help") },
      { "ksail down", await GetHelpTextAsync(ksailCommand, "down", "--help") },
      { "ksail update", await GetHelpTextAsync(ksailCommand, "update", "--help") },
      { "ksail start", await GetHelpTextAsync(ksailCommand, "start", "--help") },
      { "ksail stop", await GetHelpTextAsync(ksailCommand, "stop", "--help") },
      { "ksail init", await GetHelpTextAsync(ksailCommand, "init", "--help") },
      { "ksail lint", await GetHelpTextAsync(ksailCommand, "lint", "--help") },
      { "ksail list", await GetHelpTextAsync(ksailCommand, "list", "--help") },
      { "ksail debug", await GetHelpTextAsync(ksailCommand, "debug", "--help") },
      { "ksail gen", await GetHelpTextAsync(ksailCommand, "gen", "--help") },
      { "ksail gen cert-manager", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "--help") },
      { "ksail gen cert-manager certificate", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "certificate", "--help") },
      { "ksail gen cert-manager cluster-issuer", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "cluster-issuer", "--help") },
      { "ksail gen config", await GetHelpTextAsync(ksailCommand, "gen", "config", "--help") },
      { "ksail gen config k3d", await GetHelpTextAsync(ksailCommand, "gen", "config", "k3d", "--help") },
      { "ksail gen config ksail", await GetHelpTextAsync(ksailCommand, "gen", "config", "ksail", "--help") },
      { "ksail gen config sops", await GetHelpTextAsync(ksailCommand, "gen", "config", "sops", "--help") },
      { "ksail gen flux", await GetHelpTextAsync(ksailCommand, "gen", "flux", "--help") },
      { "ksail gen flux helm-release", await GetHelpTextAsync(ksailCommand, "gen", "flux", "helm-release", "--help") },
      { "ksail gen flux helm-repository", await GetHelpTextAsync(ksailCommand, "gen", "flux", "helm-repository", "--help") },
      { "ksail gen flux kustomization", await GetHelpTextAsync(ksailCommand, "gen", "flux", "kustomization", "--help") },
      { "ksail gen kustomize", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "--help") },
      { "ksail gen kustomize component", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "component", "--help") },
      { "ksail gen kustomize kustomization", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "kustomization", "--help") },
      { "ksail gen native", await GetHelpTextAsync(ksailCommand, "gen", "native", "--help") },
      { "ksail gen native cluster-role-binding", await GetHelpTextAsync(ksailCommand, "gen", "native", "cluster-role-binding", "--help") },
      { "ksail gen native cluster-role", await GetHelpTextAsync(ksailCommand, "gen", "native", "cluster-role", "--help") },
      { "ksail gen native namespace", await GetHelpTextAsync(ksailCommand, "gen", "native", "namespace", "--help") },
      { "ksail gen native network-policy", await GetHelpTextAsync(ksailCommand, "gen", "native", "network-policy", "--help") },
      { "ksail gen native persistent-volume", await GetHelpTextAsync(ksailCommand, "gen", "native", "persistent-volume", "--help") },
      { "ksail gen native resource-quota", await GetHelpTextAsync(ksailCommand, "gen", "native", "resource-quota", "--help") },
      { "ksail gen native role-binding", await GetHelpTextAsync(ksailCommand, "gen", "native", "role-binding", "--help") },
      { "ksail gen native role", await GetHelpTextAsync(ksailCommand, "gen", "native", "role", "--help") },
      { "ksail gen native service-account", await GetHelpTextAsync(ksailCommand, "gen", "native", "service-account", "--help") },
      { "ksail gen native config-map", await GetHelpTextAsync(ksailCommand, "gen", "native", "config-map", "--help") },
      { "ksail gen native persistent-volume-claim", await GetHelpTextAsync(ksailCommand, "gen", "native", "persistent-volume-claim", "--help") },
      { "ksail gen native secret", await GetHelpTextAsync(ksailCommand, "gen" , "native", "secret" , "--help") },
      { "ksail gen native horizontal-pod-autoscaler", await GetHelpTextAsync(ksailCommand, "gen", "native", "horizontal-pod-autoscaler", "--help") },
      { "ksail gen native pod-disruption-budget", await GetHelpTextAsync(ksailCommand, "gen", "native", "pod-disruption-budget", "--help") },
      { "ksail gen native priority-class", await GetHelpTextAsync(ksailCommand, "gen", "native", "priority-class", "--help") },
      { "ksail gen native ingress", await GetHelpTextAsync(ksailCommand, "gen", "native", "ingress", "--help") },
      { "ksail gen native service", await GetHelpTextAsync(ksailCommand, "gen", "native", "service", "--help") },
      { "ksail gen native cron-job", await GetHelpTextAsync(ksailCommand, "gen", "native", "cron-job", "--help") },
      { "ksail gen native daemon-set", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "daemon-set" , "--help") },
      { "ksail gen native deployment", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "deployment" , "--help") },
      { "ksail gen native job", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "job" , "--help") },
      { "ksail gen native stateful-set", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "stateful-set" , "--help") },
      { "ksail secrets", await GetHelpTextAsync(ksailCommand, "secrets", "--help") },
      { "ksail secrets encrypt", await GetHelpTextAsync(ksailCommand, "secrets", "encrypt", "--help") },
      { "ksail secrets decrypt", await GetHelpTextAsync(ksailCommand, "secrets", "decrypt", "--help") },
      { "ksail secrets edit", await GetHelpTextAsync(ksailCommand, "secrets", "edit", "--help") },
      { "ksail secrets add", await GetHelpTextAsync(ksailCommand, "secrets", "add", "--help") },
      { "ksail secrets rm", await GetHelpTextAsync(ksailCommand, "secrets", "rm", "--help") },
      { "ksail secrets list", await GetHelpTextAsync(ksailCommand, "secrets", "list", "--help") },
      { "ksail secrets import", await GetHelpTextAsync(ksailCommand, "secrets", "import", "--help") },
      { "ksail secrets export", await GetHelpTextAsync(ksailCommand, "secrets", "export", "--help") }
    };

    // Act
    string markdown = GenerateMarkdown(helpTexts);

    // Assert
    Assert.NotNull(markdown);
    Assert.NotEmpty(markdown);
    Assert.Contains("# KSail CLI Options", markdown, StringComparison.Ordinal);
    foreach (var (command, helpText) in helpTexts)
    {
      Assert.Contains($"## `{command}`", markdown, StringComparison.Ordinal);
      Assert.NotNull(helpText);
      Assert.NotEmpty(helpText);
      string fixedHelpText = helpText.Replace("testhost", "ksail", StringComparison.Ordinal);
      Assert.Contains($"""
      ```text
      {fixedHelpText}
      ```
      """, markdown, StringComparison.Ordinal);
    }

    // Write to file
    await File.WriteAllTextAsync("../../../../../../docs/configuration/cli-options.md", markdown);
  }

  static async Task<string?> GetHelpTextAsync(KSailRootCommand command, params string[] args)
  {
    var console = new TestConsole();
    int exitCode = await command.InvokeAsync(args, console).ConfigureAwait(false);
    Assert.Equal(0, exitCode);
    return console.Out.ToString()?.Trim();
  }

  static string GenerateMarkdown(Dictionary<string, string?> helpTexts)
  {
    string markdown = """
    ---
    title: CLI Options
    parent: Configuration
    layout: default
    nav_order: 0
    ---

    # KSail CLI Options

    > [!IMPORTANT]
    > This document is auto-generated, and is always up-to-date with the latest version of the KSail CLI.

    KSail supports CLI options for configuring the behavior of KSail. These options can be used to override the default settings, or to alter the behavior of KSail in specific ways.

    """;

    foreach (var (command, helpText) in helpTexts)
    {
      markdown += $"""

      ## `{command}`

      ```text
      {helpText}
      ```

      """;
    }

    return markdown.Replace("testhost", "ksail", StringComparison.Ordinal);
  }
}
