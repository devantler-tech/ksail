using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Handlers;
using KSail.Commands.Secrets.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsListCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly ShowPrivateKeysOption _showPrivateKeysOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ShowProjectKeysOption _showProjectKeysOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailSecretsListCommand(GlobalOptions globalOptions) : base("list", "List keys")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        config.UpdateConfig("Spec.CLI.Secrets.List.ShowPrivateKeys", context.ParseResult.GetValueForOption(_showPrivateKeysOption));
        config.UpdateConfig("Spec.CLI.Secrets.List.ShowProjectKeys", context.ParseResult.GetValueForOption(_showProjectKeysOption));

        var cancellationToken = context.GetCancellationToken();
        KSailSecretsListCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case Models.Project.KSailSecretManager.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case Models.Project.KSailSecretManager.SOPS:
            handler = new KSailSecretsListCommandHandler(config, new SOPSLocalAgeSecretManager());
            break;
        }
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(_showPrivateKeysOption);
    AddOption(_showProjectKeysOption);
  }
}
