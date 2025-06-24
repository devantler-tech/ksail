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
  readonly GenericPathOption _outputOption = new(string.Empty) { Arity = ArgumentArity.ZeroOrOne };

  internal KSailSecretsEncryptCommand() : base("encrypt", "Encrypt a file")
  {
    AddArgument(_pathArgument);
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        string path = context.ParseResult.GetValueForArgument(_pathArgument);
        string? output = context.ParseResult.GetValueForOption(_outputOption);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsEncryptCommandHandler(config, path, output, new SOPSLocalAgeSecretManager());
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
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
    AddOption(CLIOptions.SecretManager.SOPS.PublicKeyOption);
    AddOption(CLIOptions.SecretManager.SOPS.InPlaceOption);
    AddOption(_outputOption);
  }
}
