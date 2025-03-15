
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Config;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Config;

class KSailGenConfigK3dCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("./k3d.yaml");

  public KSailGenConfigK3dCommand() : base("k3d", "Generate a 'k3d.io/v1alpha5/Simple' resource.")
  {
    AddOption(_outputOption);
    this.SetHandler(async (context) =>
    {
      try
      {
        string outputFile = context.ParseResult.CommandResult.GetValueForOption(_outputOption) ?? "./k3d.yaml";
        bool overwrite = context.ParseResult.CommandResult.GetValueForOption(CLIOptions.Generator.OverwriteOption) ?? false;
        Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
          $"✚ overwriting '{outputFile}'" :
          $"✔ skipping '{outputFile}', as it already exists.") :
          $"✚ generating '{outputFile}'");
        var handler = new KSailGenConfigK3dCommandHandler(outputFile, overwrite);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}




