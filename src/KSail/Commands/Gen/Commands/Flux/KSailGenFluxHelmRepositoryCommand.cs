
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Flux;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Flux;

class KSailGenFluxHelmRepositoryCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("--output", ["-o"], "./helm-repository.yaml");
  public KSailGenFluxHelmRepositoryCommand() : base("helm-repository", "Generate a 'source.toolkit.fluxcd.io/v1/HelmRepository' resource.")
  {
    Options.Add(_outputOption);

    SetAction(async (parseResult, cancellationToken) =>
      {
        try
        {
          string outputFile = parseResult.GetValue(_outputOption) ?? "./helm-repository.yaml";
          bool overwrite = parseResult.CommandResult.GetValue(CLIOptions.Generator.OverwriteOption) ?? false;
          Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
            $"✚ overwriting '{outputFile}'" :
            $"✔ skipping '{outputFile}', as it already exists.") :
            $"✚ generating '{outputFile}'");
          if (File.Exists(outputFile) && !overwrite)
          {
            return 0;
          }
          var handler = new KSailGenFluxHelmRepositoryCommandHandler(outputFile, overwrite);
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
