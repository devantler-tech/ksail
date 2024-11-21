
using System.CommandLine;
using KSail.Commands.Gen.Handlers.Config;
using KSail.Commands.Gen.Options;
using KSail.Utils;

namespace KSail.Commands.Gen.Commands.Config;

class KSailGenConfigKSailCommand : Command
{
  readonly FileOutputOption _fileOutputOption = new("./ksail-config.yaml");
  readonly KSailGenConfigKSailCommandHandler _handler = new();
  public KSailGenConfigKSailCommand() : base("ksail", "Generate a 'ksail.io/v1alpha1/Cluster' resource.")
  {
    AddOption(_fileOutputOption);
    this.SetHandler(async (context) =>
      {
        try
        {
          string outputFile = context.ParseResult.GetValueForOption(_fileOutputOption)!;
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
