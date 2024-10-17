using System.CommandLine;
using KSail.Commands.Check.Handlers;
using KSail.Deserializer;
using KSail.Options;

namespace KSail.Commands.Check;

sealed class KSailCheckCommand : Command
{
  readonly KSailClusterDeserializer _deserializer = new();
  readonly KubeconfigOption _kubeconfigOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ContextOption _contextOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly TimeoutOption _timeoutOption = new() { Arity = ArgumentArity.ZeroOrOne };

  internal KSailCheckCommand() : base("check", "Check the status of a cluster")
  {
    AddOption(_kubeconfigOption);
    AddOption(_contextOption);
    AddOption(_timeoutOption);
    AddValidator(result =>
    {
      string? kubeconfig = result.GetValueForOption(_kubeconfigOption);
      if (string.IsNullOrWhiteSpace(kubeconfig) || !File.Exists(kubeconfig))
      {
        result.ErrorMessage = $"Kubeconfig file '{kubeconfig}' does not exist";
      }
    });
    this.SetHandler(async (context) =>
    {
      var config = await _deserializer.LocateAndDeserializeAsync().ConfigureAwait(false);
      await config.SetConfigValueAsync("Spec.Kubeconfig", context.ParseResult.GetValueForOption(_kubeconfigOption)).ConfigureAwait(false);
      await config.SetConfigValueAsync("Spec.Context", context.ParseResult.GetValueForOption(_contextOption)).ConfigureAwait(false);
      await config.SetConfigValueAsync("Spec.Timeout", context.ParseResult.GetValueForOption(_timeoutOption)).ConfigureAwait(false);

      var handler = new KSailCheckCommandHandler(config);
      try
      {
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (OperationCanceledException)
      {
        context.ExitCode = 1;
      }
    });
  }
}
