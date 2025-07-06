using System.CommandLine;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsEncryptCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to encrypt.") { Arity = ArgumentArity.ExactlyOne };
  readonly GenericPathOption _outputOption = new("--output", ["-o"], string.Empty) { Arity = ArgumentArity.ZeroOrOne };

  internal KSailSecretsEncryptCommand() : base("encrypt", "Encrypt a file")
  {
    Arguments.Add(_pathArgument);
    AddOptions();
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        string path = parseResult.GetValue(_pathArgument) ?? throw new KSailException("path is required");
        string? output = parseResult.GetValue(_outputOption);
        var handler = new KSailSecretsEncryptCommandHandler(config, path, output, new SOPSLocalAgeSecretManager());
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        Console.WriteLine();
        return 0;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        return 1;
      }
    });
  }

  void AddOptions()
  {
    Options.Add(CLIOptions.SecretManager.SOPS.PublicKeyOption);
    Options.Add(CLIOptions.SecretManager.SOPS.InPlaceOption);
    Options.Add(_outputOption);
  }
}
