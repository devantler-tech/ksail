
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Native.Metadata;
using KSail.Commands.Gen.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Native.Metadata;

class KSailGenNativeMetadataHorizontalPodAutoscalerCommand : Command
{
  readonly FileOutputOption _outputOption = new("./horizontal-pod-autoscaler.yaml");
  readonly KSailGenNativeMetadataHorizontalPodAutoscalerCommandHandler _handler = new();
  public KSailGenNativeMetadataHorizontalPodAutoscalerCommand() : base("horizontal-pod-autoscaler", "Generate a 'autoscaling/v2/HorizontalPodAutoscaler' resource.")
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
