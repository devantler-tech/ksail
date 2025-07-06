
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Kustomize;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Kustomize;

class KSailGenKustomizeComponentCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("--output", ["-o"], "./kustomization.yaml");
  internal KSailGenKustomizeComponentCommand() : base("component", "Generate a 'kustomize.config.k8s.io/v1alpha1/Component' resource.")
  {
    Options.Add(_outputOption);
    SetAction(async (parseResult, cancellationToken) =>
      {
        try
        {
          string outputFile = parseResult.GetValue(_outputOption) ?? "./kustomization.yaml";
          bool overwrite = parseResult.CommandResult.GetValue(CLIOptions.Generator.OverwriteOption) ?? false;
          Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
            $"✚ overwriting '{outputFile}'" :
            $"✔ skipping '{outputFile}', as it already exists.") :
            $"✚ generating '{outputFile}'");
          if (File.Exists(outputFile) && !overwrite)
          {
            return 0;
          }
          var handler = new KSailGenKustomizeComponentCommandHandler(outputFile, overwrite);
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
