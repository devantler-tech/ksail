using System.CommandLine;
using KSail.Commands.SOPS.Handlers;
using KSail.Commands.SOPS.Options;
using KSail.Utils;
using YamlDotNet.Core;

namespace KSail.Commands.SOPS.Commands;

sealed class KSailSOPSListCommand : Command
{
  readonly ShowPrivateKeyOption _showPrivateKeyOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ShowSOPSConfigKeysOnlyOption _showSOPSConfigKeysOnlyOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailSOPSListCommand() : base("list", "List keys")
  {
    AddOption(_showPrivateKeyOption);
    AddOption(_showSOPSConfigKeysOnlyOption);

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadAsync().ConfigureAwait(false);
        config.UpdateConfig("Spec.CLI.SopsOptions.List.ShowPrivateKey", context.ParseResult.GetValueForOption(_showPrivateKeyOption));
        config.UpdateConfig("Spec.CLI.SopsOptions.List.ShowSOPSConfigKeysOnly", context.ParseResult.GetValueForOption(_showSOPSConfigKeysOnlyOption));

        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSOPSListCommandHandler(config);

        Console.WriteLine("🔑 Listing keys");
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
        Console.WriteLine();
      }
      catch (YamlException ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
      catch (OperationCanceledException ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
      catch (KSailException ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}
