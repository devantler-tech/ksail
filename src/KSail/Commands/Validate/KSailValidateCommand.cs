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
    Options.Add(_pathOption);
    this.SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        string path = parseResult.GetValue(_pathOption) ?? "./";
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context, path).ConfigureAwait(false);
        var handler = new KSailValidateCommandHandler(config, path);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }
}
