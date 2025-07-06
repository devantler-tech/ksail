
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Native;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Native;

class KSailGenNativeWorkloadsCronJobCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("--output", ["-o"], "./cron-job.yaml");
  public KSailGenNativeWorkloadsCronJobCommand() : base("cron-job", "Generate a 'batch/v1/CronJob' resource.")
  {
    Options.Add(_outputOption);
    SetAction(async (parseResult, cancellationToken) =>
      {
        try
        {
          string outputFile = parseResult.GetValue(_outputOption) ?? "./cron-job.yaml";
          bool overwrite = parseResult.CommandResult.GetValue(CLIOptions.Generator.OverwriteOption) ?? false;
          Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
            $"✚ overwriting '{outputFile}'" :
            $"✔ skipping '{outputFile}', as it already exists.") :
            $"✚ generating '{outputFile}'");
          if (File.Exists(outputFile) && !overwrite)
          {
            return 0;
          }
          KSailGenNativeWorkloadsCronJobCommandHandler handler = new(outputFile, overwrite);
          await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
          return 0;
        }
        catch (Exception ex)
        {
          _ = _exceptionHandler.HandleException(ex);
          return 1;
        }
      }
    );
  }
}
