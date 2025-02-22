using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Commands.Secrets.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsDecryptCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to decrypt.") { Arity = ArgumentArity.ExactlyOne };
  readonly InPlaceOption _inPlaceOption = new("Decrypt the file in place.") { Arity = ArgumentArity.ZeroOrOne };
  readonly OutputOption _outputOption = new(string.Empty, "The path to output the decrypted file.") { Arity = ArgumentArity.ZeroOrOne };

  internal KSailSecretsDecryptCommand(GlobalOptions globalOptions) : base("decrypt", "Decrypt a file")
  {
    AddArgument(_pathArgument);
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        string path = context.ParseResult.GetValueForArgument(_pathArgument);
        bool inPlace = context.ParseResult.GetValueForOption(_inPlaceOption);
        string? output = context.ParseResult.GetValueForOption(_outputOption);
        var cancellationToken = context.GetCancellationToken();
        KSailSecretsDecryptCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case Models.Project.KSailSecretManager.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case Models.Project.KSailSecretManager.SOPS:
            handler = new KSailSecretsDecryptCommandHandler(path, inPlace, output, new SOPSLocalAgeSecretManager());
            break;
        }
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
        Console.WriteLine();
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(_inPlaceOption);
    AddOption(_outputOption);
  }
}
