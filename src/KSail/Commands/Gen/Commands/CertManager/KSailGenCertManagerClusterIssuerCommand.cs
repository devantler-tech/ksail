
using System.CommandLine;
using KSail.Commands.Gen.Handlers.CertManager;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.CertManager;

class KSailGenCertManagerClusterIssuerCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GenericPathOption _outputOption = new("./cluster-issuer.yaml");

  public KSailGenCertManagerClusterIssuerCommand() : base("cluster-issuer", "Generate a 'cert-manager.io/v1/ClusterIssuer' resource.")
  {
    AddOption(_outputOption);

    this.SetHandler(async (context) =>
      {
        try
        {
          string outputFile = context.ParseResult.RootCommandResult.GetValueForOption(_outputOption)!;
          var handler = new KSailGenCertManagerClusterIssuerCommandHandler();
          Console.WriteLine($"✚ generating {outputFile}");
          context.ExitCode = await handler.HandleAsync(outputFile, context.GetCancellationToken()).ConfigureAwait(false);
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
