
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Native;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Native;

class KSailGenNativeResourceQuotaCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("./resource-quota.yaml");
  public KSailGenNativeResourceQuotaCommand() : base("resource-quota", "Generate a 'core/v1/ResourceQuota' resource.")
  {
    AddOption(_outputOption);
    this.SetHandler(async (context) =>
      {
        try
        {
          string outputFile = context.ParseResult.GetValueForOption(_outputOption) ?? "./resource-quota.yaml";
          bool overwrite = context.ParseResult.CommandResult.GetValueForOption(CLIOptions.Generator.OverwriteOption) ?? false;
          Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
            $"✚ overwriting '{outputFile}'" :
            $"✔ skipping '{outputFile}', as it already exists.") :
            $"✚ generating '{outputFile}'");
          if (File.Exists(outputFile) && !overwrite)
          {
            return;
          }
          KSailGenNativeResourceQuotaCommandHandler handler = new(outputFile, overwrite);
          context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
          _ = _exceptionHandler.HandleException(ex);
          context.ExitCode = 1;
        }
      }
    );
  }
}
