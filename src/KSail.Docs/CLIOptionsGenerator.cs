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
    var ksailCommand = new CommandLineBuilder(new KSailRootCommand(new SystemConsole()))
      .UseVersionOption()
      .UseHelp("--helpz")
      .UseEnvironmentVariableDirective()
      .UseParseDirective()
      .UseSuggestDirective()
      .RegisterWithDotnetSuggest()
      .UseTypoCorrections()
      .UseParseErrorReporting()
      .UseExceptionHandler()
      .CancelOnProcessTermination()
      .Build();
    var helpTexts = new Dictionary<string, string?>
    {
      { "ksail", await GetHelpTextAsync(ksailCommand, "--helpz").ConfigureAwait(false) },
      { "ksail init", await GetHelpTextAsync(ksailCommand, "init", "--helpz").ConfigureAwait(false) },
      { "ksail up", await GetHelpTextAsync(ksailCommand, "up", "--helpz").ConfigureAwait(false) },
      { "ksail update", await GetHelpTextAsync(ksailCommand, "update", "--helpz").ConfigureAwait(false) },
      { "ksail start", await GetHelpTextAsync(ksailCommand, "start", "--helpz").ConfigureAwait(false) },
      { "ksail stop", await GetHelpTextAsync(ksailCommand, "stop", "--helpz").ConfigureAwait(false) },
      { "ksail down", await GetHelpTextAsync(ksailCommand, "down", "--helpz").ConfigureAwait(false) },
      { "ksail status", await GetHelpTextAsync(ksailCommand, "status", "--helpz").ConfigureAwait(false) },
      { "ksail list", await GetHelpTextAsync(ksailCommand, "list", "--helpz").ConfigureAwait(false) },
      { "ksail validate", await GetHelpTextAsync(ksailCommand, "validate", "--helpz").ConfigureAwait(false) },
      { "ksail connect", await GetHelpTextAsync(ksailCommand, "connect", "--helpz").ConfigureAwait(false) },
      { "ksail gen", await GetHelpTextAsync(ksailCommand, "gen", "--helpz").ConfigureAwait(false) },
      { "ksail gen cert-manager", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "--helpz").ConfigureAwait(false) },
      { "ksail gen cert-manager certificate", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "certificate", "--helpz").ConfigureAwait(false) },
      { "ksail gen cert-manager cluster-issuer", await GetHelpTextAsync(ksailCommand, "gen", "cert-manager", "cluster-issuer", "--helpz").ConfigureAwait(false) },
      { "ksail gen config", await GetHelpTextAsync(ksailCommand, "gen", "config", "--helpz").ConfigureAwait(false) },
      { "ksail gen config k3d", await GetHelpTextAsync(ksailCommand, "gen", "config", "k3d", "--helpz").ConfigureAwait(false) },
      { "ksail gen config ksail", await GetHelpTextAsync(ksailCommand, "gen", "config", "ksail", "--helpz").ConfigureAwait(false) },
      { "ksail gen config sops", await GetHelpTextAsync(ksailCommand, "gen", "config", "sops", "--helpz").ConfigureAwait(false) },
      { "ksail gen flux", await GetHelpTextAsync(ksailCommand, "gen", "flux", "--helpz").ConfigureAwait(false) },
      { "ksail gen flux helm-release", await GetHelpTextAsync(ksailCommand, "gen", "flux", "helm-release", "--helpz").ConfigureAwait(false) },
      { "ksail gen flux helm-repository", await GetHelpTextAsync(ksailCommand, "gen", "flux", "helm-repository", "--helpz").ConfigureAwait(false) },
      { "ksail gen flux kustomization", await GetHelpTextAsync(ksailCommand, "gen", "flux", "kustomization", "--helpz").ConfigureAwait(false) },
      { "ksail gen kustomize", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "--helpz").ConfigureAwait(false) },
      { "ksail gen kustomize component", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "component", "--helpz").ConfigureAwait(false) },
      { "ksail gen kustomize kustomization", await GetHelpTextAsync(ksailCommand, "gen", "kustomize", "kustomization", "--helpz").ConfigureAwait(false) },
      { "ksail gen native", await GetHelpTextAsync(ksailCommand, "gen", "native", "--helpz").ConfigureAwait(false) },
      { "ksail gen native cluster-role-binding", await GetHelpTextAsync(ksailCommand, "gen", "native", "cluster-role-binding", "--helpz").ConfigureAwait(false) },
      { "ksail gen native cluster-role", await GetHelpTextAsync(ksailCommand, "gen", "native", "cluster-role", "--helpz").ConfigureAwait(false) },
      { "ksail gen native namespace", await GetHelpTextAsync(ksailCommand, "gen", "native", "namespace", "--helpz").ConfigureAwait(false) },
      { "ksail gen native network-policy", await GetHelpTextAsync(ksailCommand, "gen", "native", "network-policy", "--helpz").ConfigureAwait(false) },
      { "ksail gen native persistent-volume", await GetHelpTextAsync(ksailCommand, "gen", "native", "persistent-volume", "--helpz").ConfigureAwait(false) },
      { "ksail gen native resource-quota", await GetHelpTextAsync(ksailCommand, "gen", "native", "resource-quota", "--helpz").ConfigureAwait(false) },
      { "ksail gen native role-binding", await GetHelpTextAsync(ksailCommand, "gen", "native", "role-binding", "--helpz").ConfigureAwait(false) },
      { "ksail gen native role", await GetHelpTextAsync(ksailCommand, "gen", "native", "role", "--helpz").ConfigureAwait(false) },
      { "ksail gen native service-account", await GetHelpTextAsync(ksailCommand, "gen", "native", "service-account", "--helpz").ConfigureAwait(false) },
      { "ksail gen native config-map", await GetHelpTextAsync(ksailCommand, "gen", "native", "config-map", "--helpz").ConfigureAwait(false) },
      { "ksail gen native persistent-volume-claim", await GetHelpTextAsync(ksailCommand, "gen", "native", "persistent-volume-claim", "--helpz").ConfigureAwait(false) },
      { "ksail gen native secret", await GetHelpTextAsync(ksailCommand, "gen" , "native", "secret" , "--helpz").ConfigureAwait(false) },
      { "ksail gen native horizontal-pod-autoscaler", await GetHelpTextAsync(ksailCommand, "gen", "native", "horizontal-pod-autoscaler", "--helpz").ConfigureAwait(false) },
      { "ksail gen native pod-disruption-budget", await GetHelpTextAsync(ksailCommand, "gen", "native", "pod-disruption-budget", "--helpz").ConfigureAwait(false) },
      { "ksail gen native priority-class", await GetHelpTextAsync(ksailCommand, "gen", "native", "priority-class", "--helpz").ConfigureAwait(false) },
      { "ksail gen native ingress", await GetHelpTextAsync(ksailCommand, "gen", "native", "ingress", "--helpz").ConfigureAwait(false) },
      { "ksail gen native service", await GetHelpTextAsync(ksailCommand, "gen", "native", "service", "--helpz").ConfigureAwait(false) },
      { "ksail gen native cron-job", await GetHelpTextAsync(ksailCommand, "gen", "native", "cron-job", "--helpz").ConfigureAwait(false) },
      { "ksail gen native daemon-set", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "daemon-set" , "--helpz").ConfigureAwait(false) },
      { "ksail gen native deployment", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "deployment" , "--helpz").ConfigureAwait(false) },
      { "ksail gen native job", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "job" , "--helpz").ConfigureAwait(false) },
      { "ksail gen native stateful-set", await GetHelpTextAsync(ksailCommand, "gen" , "native" , "stateful-set" , "--helpz").ConfigureAwait(false) },
      { "ksail secrets", await GetHelpTextAsync(ksailCommand, "secrets", "--helpz").ConfigureAwait(false) },
      { "ksail secrets encrypt", await GetHelpTextAsync(ksailCommand, "secrets", "encrypt", "--helpz").ConfigureAwait(false) },
      { "ksail secrets decrypt", await GetHelpTextAsync(ksailCommand, "secrets", "decrypt", "--helpz").ConfigureAwait(false) },
      { "ksail secrets edit", await GetHelpTextAsync(ksailCommand, "secrets", "edit", "--helpz").ConfigureAwait(false) },
      { "ksail secrets add", await GetHelpTextAsync(ksailCommand, "secrets", "add", "--helpz").ConfigureAwait(false) },
      { "ksail secrets rm", await GetHelpTextAsync(ksailCommand, "secrets", "rm", "--helpz").ConfigureAwait(false) },
      { "ksail secrets list", await GetHelpTextAsync(ksailCommand, "secrets", "list", "--helpz").ConfigureAwait(false) },
      { "ksail secrets import", await GetHelpTextAsync(ksailCommand, "secrets", "import", "--helpz").ConfigureAwait(false) },
      { "ksail secrets export", await GetHelpTextAsync(ksailCommand, "secrets", "export", "--helpz").ConfigureAwait(false) },
      { "ksail run", await GetHelpTextAsync(ksailCommand, "run", "--helpz").ConfigureAwait(false) }
    };

    return GenerateMarkdown(helpTexts);
  }

  static async Task<string?> GetHelpTextAsync(Parser command, params string[] args)
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
