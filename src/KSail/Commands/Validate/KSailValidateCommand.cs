using System.CommandLine;
using KSail.Commands.Validate.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Validate;

sealed class KSailValidateCommand : Command
{
  readonly GenericPathOption _pathOption = new("./", ["-p", "--path"])
  {
    Description = "Path to the project files. [default: ./]"
  };

  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailValidateCommand() : base(
   "validate", "Validate project files"
  )
  {
    AddOption(_pathOption);
    this.SetHandler(async (context) =>
    {
      try
      {
        string path = context.ParseResult.GetValueForOption(_pathOption) ?? "./";
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context, path).ConfigureAwait(false);
        var handler = new KSailValidateCommandHandler(config, path);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}
