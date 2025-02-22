using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Commands.Secrets.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsEncryptCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to encrypt.") { Arity = ArgumentArity.ExactlyOne };
  readonly PublicKeyOption _publicKeyOption = new("The public key to encrypt the file with.") { Arity = ArgumentArity.ZeroOrOne };
  readonly InPlaceOption _inPlaceOption = new("Encrypt the file in place.") { Arity = ArgumentArity.ZeroOrOne };
  readonly OutputOption _outputOption = new(string.Empty, "The path to output the encrypted file.") { Arity = ArgumentArity.ZeroOrOne };

  internal KSailSecretsEncryptCommand(GlobalOptions globalOptions) : base("encrypt", "Encrypt a file")
  {
    AddArgument(_pathArgument);
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        string path = context.ParseResult.GetValueForArgument(_pathArgument);
        string? publicKey = context.ParseResult.GetValueForOption(_publicKeyOption);
        bool inPlace = context.ParseResult.GetValueForOption(_inPlaceOption);
        string? output = context.ParseResult.GetValueForOption(_outputOption);
        var cancellationToken = context.GetCancellationToken();
        KSailSecretsEncryptCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case Models.Project.KSailSecretManager.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case Models.Project.KSailSecretManager.SOPS:
            handler = new KSailSecretsEncryptCommandHandler(path, publicKey, inPlace, output, new SOPSLocalAgeSecretManager());
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
    AddOption(_publicKeyOption);
    AddOption(_inPlaceOption);
    AddOption(_outputOption);
  }
}
