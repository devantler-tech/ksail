
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Native;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Native;

class KSailGenNativeWorkloadsStatefulSetCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("--output", ["-o"], "./stateful-set.yaml");
  public KSailGenNativeWorkloadsStatefulSetCommand() : base("stateful-set", "Generate a 'apps/v1/StatefulSet' resource.")
  {
    Options.Add(_outputOption);
    SetAction(async (parseResult, cancellationToken) =>
      {
        try
        {
          string outputFile = parseResult.GetValue(_outputOption) ?? "./stateful-set.yaml";
          bool overwrite = parseResult.CommandResult.GetValue(CLIOptions.Generator.OverwriteOption) ?? false;
          Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
            $"✚ overwriting '{outputFile}'" :
            $"✔ skipping '{outputFile}', as it already exists.") :
            $"✚ generating '{outputFile}'");
          if (File.Exists(outputFile) && !overwrite)
          {
            return;
          }
          KSailGenNativeWorkloadsStatefulSetCommandHandler handler = new(outputFile, overwrite);
          await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
          _ = _exceptionHandler.HandleException(ex);

        }
      }
    );
  }
}
