using System.CommandLine;
using KSail.Commands.Stop.Handlers;
using KSail.Options;
using KSail.Utils;
using YamlDotNet.Core;

namespace KSail.Commands.Stop;

sealed class KSailStopCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly MetadataNameOption _nameOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ProjectEngineOption _engineOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ProjectDistributionOption _distributionOption = new() { Arity = ArgumentArity.ZeroOrOne };

  internal KSailStopCommand() : base("stop", "Stop a cluster")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      var config = await KSailClusterConfigLoader.LoadAsync(name: context.ParseResult.GetValueForOption(_nameOption)).ConfigureAwait(false);
      config.UpdateConfig("Metadata.Name", context.ParseResult.GetValueForOption(_nameOption));
      config.UpdateConfig("Spec.Project.Engine", context.ParseResult.GetValueForOption(_engineOption));
      config.UpdateConfig("Spec.Project.Distribution", context.ParseResult.GetValueForOption(_distributionOption));

      var handler = new KSailStopCommandHandler(config);
      try
      {
        Console.WriteLine($"🛑 Stopping cluster '{config.Spec.Connection.Context}'");
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
        if (context.ExitCode == 0)
        {
          Console.WriteLine("✔ Cluster stopped");
        }
        else
        {
          throw new KSailException("Cluster could not be stopped");
        }
      }
      catch (YamlException ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
      catch (KSailException ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
      catch (OperationCanceledException)
      {
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(_nameOption);
    AddOption(_engineOption);
    AddOption(_distributionOption);
  }
}
