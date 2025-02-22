
using System.CommandLine;
using KSail.Commands.Gen.Handlers.CertManager;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.CertManager;

class KSailGenCertManagerCertificateCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly OutputOption _outputOption = new("./certificate.yaml");
  public KSailGenCertManagerCertificateCommand() : base("certificate", "Generate a 'cert-manager.io/v1/Certificate' resource.")
  {
    this.AddOption(_outputOption);

    this.SetHandler(async (context) =>
      {
        try
        {
          string outputFile = context.ParseResult.CommandResult.GetValueForOption(_outputOption) ?? "./certificate.yaml";
          var handler = new KSailGenCertManagerCertificateCommandHandler();
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
