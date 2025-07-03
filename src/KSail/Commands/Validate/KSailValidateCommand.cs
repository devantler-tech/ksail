using System.CommandLine;
using KSail.Commands.Validate.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Validate;

sealed class KSailValidateCommand : Command
{
  readonly GenericPathOption _pathOption = new("--path", ["-p"], "./")
  {
    Description = "Path to the project files."
  };

  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailValidateCommand() : base(
   "validate", "Validate project files"
  )
  {
    Options.Add(_pathOption);
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        string path = parseResult.GetValue(_pathOption) ?? "./";
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult, path).ConfigureAwait(false);
        var handler = new KSailValidateCommandHandler(config, path);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        return 0;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        return 1;
      }
    });
  }
}
