using System.CommandLine;
using Devantler.FluxCLI;
using Devantler.KindCLI;
using KSail.Commands.Up.Handlers;
using KSail.Commands.Up.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Up;

sealed class KSailUpCommand : Command
{
  readonly NameOption _nameOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly DestroyOption _destroyOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ConfigOption _configOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly PathOption _manifestsPathOption = new("Path to the manifests directory") { Arity = ArgumentArity.ZeroOrOne };
  readonly PathOption _kustomizationDirectoryOption = new("Path to the root kustomization directory", ["--kustomization-path", "-kp"]) { Arity = ArgumentArity.ZeroOrOne };
  readonly TimeoutOption _timeoutOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly SOPSOption _sopsOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly LintOption _lintOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailUpCommand() : base("up", "Provision a cluster")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      var config = await KSailClusterConfigLoader.LoadAsync(context.ParseResult.GetValueForOption(_manifestsPathOption), context.ParseResult.GetValueForOption(_nameOption)).ConfigureAwait(false);
      config.UpdateConfig("Metadata.Name", context.ParseResult.GetValueForOption(_nameOption));
      config.UpdateConfig("Spec.ConfigPath", context.ParseResult.GetValueForOption(_configOption));
      config.UpdateConfig("Spec.ManifestsDirectory", context.ParseResult.GetValueForOption(_manifestsPathOption));

      string? kustomizationDirectory = context.ParseResult.GetValueForOption(_kustomizationDirectoryOption);
      if (kustomizationDirectory != null && !string.IsNullOrEmpty(kustomizationDirectory) && !kustomizationDirectory.Equals("default", StringComparison.OrdinalIgnoreCase))
        config.UpdateConfig("Spec.KustomizationDirectory", kustomizationDirectory);

      config.UpdateConfig("Spec.Timeout", context.ParseResult.GetValueForOption(_timeoutOption));
      config.UpdateConfig("Spec.Sops", context.ParseResult.GetValueForOption(_sopsOption));
      config.UpdateConfig("Spec.UpOptions.Destroy", context.ParseResult.GetValueForOption(_destroyOption));
      config.UpdateConfig("Spec.UpOptions.Lint", context.ParseResult.GetValueForOption(_lintOption));

      var handler = new KSailUpCommandHandler(config);
      try
      {
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (OperationCanceledException ex)
      {
        ExceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
      catch (KindException ex)
      {
        ExceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
      catch (FluxException ex)
      {
        ExceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(_nameOption);
    AddOption(_destroyOption);
    AddOption(_configOption);
    AddOption(_manifestsPathOption);
    AddOption(_kustomizationDirectoryOption);
    AddOption(_timeoutOption);
    AddOption(_sopsOption);
    AddOption(_lintOption);
  }
}
