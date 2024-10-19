
using System.CommandLine;
using KSail.Commands.Gen.Handlers.CertManager;
using KSail.Commands.Gen.Options;

namespace KSail.Commands.Gen.Commands.CertManager;

class KSailGenCertManagerCertificateCommand : Command
{
  readonly FileOutputOption _outputOption = new("./certificate.yaml");
  public KSailGenCertManagerCertificateCommand() : base("certificate", "Generate a 'cert-manager.io/v1/Certificate' resource.")
  {
    AddOption(_outputOption);

    this.SetHandler(async (context) =>
      {
        string outputFile = context.ParseResult.RootCommandResult.GetValueForOption(_outputOption)!;
        var handler = new KSailGenCertManagerCertificateCommandHandler();
        try
        {
          Console.WriteLine($"✚ Generating {outputFile}");
          context.ExitCode = await handler.HandleAsync(outputFile, context.GetCancellationToken()).ConfigureAwait(false);
        }
        catch (OperationCanceledException)
        {
          context.ExitCode = 1;
        }
      }
    );
  }
}
