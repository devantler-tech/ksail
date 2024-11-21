
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Native.Services;
using KSail.Commands.Gen.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Native.Service;

class KSailGenNativeServiceIngressClassCommand : Command
{
  readonly FileOutputOption _outputOption = new("./ingress-class.yaml");
  readonly KSailGenNativeServiceIngressClassCommandHandler _handler = new();
  public KSailGenNativeServiceIngressClassCommand() : base("ingress-class", "Generate a 'networking.k8s.io/v1/IngressClass' resource.")
  {
    AddOption(_outputOption);
    this.SetHandler(async (context) =>
      {
        string outputFile = context.ParseResult.GetValueForOption(_outputOption) ?? throw new ArgumentNullException(nameof(_outputOption));
        try
        {
          Console.WriteLine($"✚ generating {outputFile}");
          context.ExitCode = await _handler.HandleAsync(outputFile, context.GetCancellationToken()).ConfigureAwait(false);
        }
        catch (OperationCanceledException ex)
        {
          ExceptionHandler.HandleException(ex);
          context.ExitCode = 1;
        }
      }
    );
  }
}
