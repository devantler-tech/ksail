
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
    Options.Add(_outputOption);
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        string outputFile = parseResult.CommandResult.GetValue(_outputOption) ?? "./k3d.yaml";
        bool overwrite = parseResult.CommandResult.GetValue(CLIOptions.Generator.OverwriteOption) ?? false;
        Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
          $"✚ overwriting '{outputFile}'" :
          $"✔ skipping '{outputFile}', as it already exists.") :
          $"✚ generating '{outputFile}'");
        var handler = new KSailGenConfigK3dCommandHandler(outputFile, overwrite);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }
}




