
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Native;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Native;

class KSailGenNativeNetworkPolicyCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("--output", ["-o"], "./network-policy.yaml");
  public KSailGenNativeNetworkPolicyCommand() : base("network-policy", "Generate a 'networking.k8s.io/v1/NetworkPolicy' resource.")
  {
    Options.Add(_outputOption);
    SetAction(async (parseResult, cancellationToken) =>
      {
        try
        {
          string outputFile = parseResult.GetValue(_outputOption) ?? "./network-policy.yaml";
          bool overwrite = parseResult.CommandResult.GetValue(CLIOptions.Generator.OverwriteOption) ?? false;
          Console.WriteLine(File.Exists(outputFile) ? (overwrite ?
            $"✚ overwriting '{outputFile}'" :
            $"✔ skipping '{outputFile}', as it already exists.") :
            $"✚ generating '{outputFile}'");
          if (File.Exists(outputFile) && !overwrite)
          {
            return 0;
          }
          KSailGenNativeNetworkPolicyCommandHandler handler = new(outputFile, overwrite);
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
