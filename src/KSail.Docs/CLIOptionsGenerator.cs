using System.CommandLine;
using System.CommandLine.Builder;
using System.CommandLine.IO;
using System.CommandLine.Parsing;
using KSail.Commands.Root;

namespace KSail.Docs;

static class CLIOptionsGenerator
{
  public static async Task<string> GenerateAsync()
  {
    // Arrange
    var console = new SystemConsole();
    var ksailCommand = new KSailRootCommand(console);
    var helpTexts = new Dictionary<string, string?>
    {
      { "ksail", await GetHelpTextAsync(ksailCommand, "--help").ConfigureAwait(false) },
      { "ksail init", await GetHelpTextAsync(ksailCommand, "init", "--help").ConfigureAwait(false) },
      { "ksail up", await GetHelpTextAsync(ksailCommand, "up", "--help").ConfigureAwait(false) },
      { "ksail update", await GetHelpTextAsync(ksailCommand, "update", "--help").ConfigureAwait(false) },
      { "ksail start", await GetHelpTextAsync(ksailCommand, "start", "--help").ConfigureAwait(false) },
      { "ksail stop", await GetHelpTextAsync(ksailCommand, "stop", "--help").ConfigureAwait(false) },
      { "ksail down", await GetHelpTextAsync(ksailCommand, "down", "--help").ConfigureAwait(false) },
      { "ksail status", await GetHelpTextAsync(ksailCommand, "status", "--help").ConfigureAwait(false) },
      { "ksail list", await GetHelpTextAsync(ksailCommand, "list", "--help").ConfigureAwait(false) },
      { "ksail validate", await GetHelpTextAsync(ksailCommand, "validate", "--help").ConfigureAwait(false) },
      { "ksail connect", await GetHelpTextAsync(ksailCommand, "connect", "--help").ConfigureAwait(false) },
      { "ksail gen", await GetHelpTextAsync(ksailCommand, "gen", "--help").ConfigureAwait(false) },
      { "ksail gen cert-manager", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "--help").ConfigureAwait(false) },
      { "ksail gen cert-manager certificate", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "certificate", "--help").ConfigureAwait(false) },
      { "ksail gen cert-manager cluster-issuer", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "cluster-issuer", "--help").ConfigureAwait(false) },
      { "ksail gen config", await GetHelpTextAsync(ksailCommand, "gen", "config", "--help").ConfigureAwait(false) },
      { "ksail gen config k3d", await GetHelpTextAsync(ksailCommand, "gen", "config", "k3d", "--help").ConfigureAwait(false) },
      { "ksail gen config ksail", await GetHelpTextAsync(ksailCommand, "gen", "config", "ksail", "--help").ConfigureAwait(false) },
      { "ksail gen config sops", await GetHelpTextAsync(ksailCommand, "gen", "config", "sops", "--help").ConfigureAwait(false) },
      { "ksail gen flux", await GetHelpTextAsync(ksailCommand, "gen", "flux", "--help").ConfigureAwait(false) },
      { "ksail gen flux helm-release", await GetHelpTextAsync(ksailCommand, "gen", "flux", "helm-release", "--help").ConfigureAwait(false) },
      { "ksail gen flux helm-repository", await GetHelpTextAsync(ksailCommand, "gen", "flux", "helm-repository", "--help").ConfigureAwait(false) },
      { "ksail gen flux kustomization", await GetHelpTextAsync(ksailCommand, "gen", "flux", "kustomization", "--help").ConfigureAwait(false) },
      { "ksail gen kustomize", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "--help").ConfigureAwait(false) },
      { "ksail gen kustomize component", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "component", "--help").ConfigureAwait(false) },
      { "ksail gen kustomize kustomization", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "kustomization", "--help").ConfigureAwait(false) },
      { "ksail gen native", await GetHelpTextAsync(ksailCommand, "gen", "native", "--help").ConfigureAwait(false) },
      { "ksail gen native cluster-role-binding", await GetHelpTextAsync(ksailCommand, "gen", "native", "cluster-role-binding", "--help").ConfigureAwait(false) },
      { "ksail gen native cluster-role", await GetHelpTextAsync(ksailCommand, "gen", "native", "cluster-role", "--help").ConfigureAwait(false) },
      { "ksail gen native namespace", await GetHelpTextAsync(ksailCommand, "gen", "native", "namespace", "--help").ConfigureAwait(false) },
      { "ksail gen native network-policy", await GetHelpTextAsync(ksailCommand, "gen", "native", "network-policy", "--help").ConfigureAwait(false) },
      { "ksail gen native persistent-volume", await GetHelpTextAsync(ksailCommand, "gen", "native", "persistent-volume", "--help").ConfigureAwait(false) },
      { "ksail gen native resource-quota", await GetHelpTextAsync(ksailCommand, "gen", "native", "resource-quota", "--help").ConfigureAwait(false) },
      { "ksail gen native role-binding", await GetHelpTextAsync(ksailCommand, "gen", "native", "role-binding", "--help").ConfigureAwait(false) },
      { "ksail gen native role", await GetHelpTextAsync(ksailCommand, "gen", "native", "role", "--help").ConfigureAwait(false) },
      { "ksail gen native service-account", await GetHelpTextAsync(ksailCommand, "gen", "native", "service-account", "--help").ConfigureAwait(false) },
      { "ksail gen native config-map", await GetHelpTextAsync(ksailCommand, "gen", "native", "config-map", "--help").ConfigureAwait(false) },
      { "ksail gen native persistent-volume-claim", await GetHelpTextAsync(ksailCommand, "gen", "native", "persistent-volume-claim", "--help").ConfigureAwait(false) },
      { "ksail gen native secret", await GetHelpTextAsync(ksailCommand, "gen" , "native", "secret" , "--help").ConfigureAwait(false) },
      { "ksail gen native horizontal-pod-autoscaler", await GetHelpTextAsync(ksailCommand, "gen", "native", "horizontal-pod-autoscaler", "--help").ConfigureAwait(false) },
      { "ksail gen native pod-disruption-budget", await GetHelpTextAsync(ksailCommand, "gen", "native", "pod-disruption-budget", "--help").ConfigureAwait(false) },
      { "ksail gen native priority-class", await GetHelpTextAsync(ksailCommand, "gen", "native", "priority-class", "--help").ConfigureAwait(false) },
      { "ksail gen native ingress", await GetHelpTextAsync(ksailCommand, "gen", "native", "ingress", "--help").ConfigureAwait(false) },
      { "ksail gen native service", await GetHelpTextAsync(ksailCommand, "gen", "native", "service", "--help").ConfigureAwait(false) },
      { "ksail gen native cron-job", await GetHelpTextAsync(ksailCommand, "gen", "native", "cron-job", "--help").ConfigureAwait(false) },
      { "ksail gen native daemon-set", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "daemon-set" , "--help").ConfigureAwait(false) },
      { "ksail gen native deployment", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "deployment" , "--help").ConfigureAwait(false) },
      { "ksail gen native job", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "job" , "--help").ConfigureAwait(false) },
      { "ksail gen native stateful-set", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "stateful-set" , "--help").ConfigureAwait(false) },
      { "ksail secrets", await GetHelpTextAsync(ksailCommand, "secrets", "--help").ConfigureAwait(false) },
      { "ksail secrets encrypt", await GetHelpTextAsync(ksailCommand, "secrets", "encrypt", "--help").ConfigureAwait(false) },
      { "ksail secrets decrypt", await GetHelpTextAsync(ksailCommand, "secrets", "decrypt", "--help").ConfigureAwait(false) },
      { "ksail secrets edit", await GetHelpTextAsync(ksailCommand, "secrets", "edit", "--help").ConfigureAwait(false) },
      { "ksail secrets add", await GetHelpTextAsync(ksailCommand, "secrets", "add", "--help").ConfigureAwait(false) },
      { "ksail secrets rm", await GetHelpTextAsync(ksailCommand, "secrets", "rm", "--help").ConfigureAwait(false) },
      { "ksail secrets list", await GetHelpTextAsync(ksailCommand, "secrets", "list", "--help").ConfigureAwait(false) },
      { "ksail secrets import", await GetHelpTextAsync(ksailCommand, "secrets", "import", "--help").ConfigureAwait(false) },
      { "ksail secrets export", await GetHelpTextAsync(ksailCommand, "secrets", "export", "--help").ConfigureAwait(false) }
    };

    return GenerateMarkdown(helpTexts);
  }

  static async Task<string?> GetHelpTextAsync(Command command, params string[] args)
  {
    var console = new TestConsole();
    _ = await command.InvokeAsync(args, console).ConfigureAwait(false);
    return console.Out.ToString()?.Trim()
      .Replace("KSail.Docs", "ksail", StringComparison.Ordinal)
      .Replace(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile) + "/", "~/", StringComparison.Ordinal);
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
    > This document is auto-generated by `src/KSail.Docs/CLIOptionsGenerator.cs` and is always up-to-date with the latest version of the KSail CLI.

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

    return markdown;
  }
}
