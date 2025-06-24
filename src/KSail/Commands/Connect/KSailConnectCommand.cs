using System.CommandLine;
using System.Diagnostics.CodeAnalysis;
using KSail.Commands.Connect.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Connect;

[ExcludeFromCodeCoverage]
sealed class KSailConnectCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailConnectCommand() : base("connect", "Connect to a cluster with K9s")
  {
    AddOptions();
    this.SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailConnectCommandHandler(config);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }

  internal void AddOptions()
  {
    Options.Add(CLIOptions.Connection.KubeconfigOption);
    Options.Add(CLIOptions.Connection.ContextOption);
    Options.Add(CLIOptions.Project.EditorOption);
  }
}
