using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsExportCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PublicKeyArgument _publicKeyArgument = new("The public key for the encryption key to export") { Arity = ArgumentArity.ExactlyOne };
  readonly GenericPathOption _outputFilePathOption = new(aliases: ["--output", "-o"]) { Arity = ArgumentArity.ExactlyOne };
  internal KSailSecretsExportCommand() : base("export", "Export a key to a file")
  {
    AddArguments();
    AddOptions();

    AddValidator(commandResult =>
    {
      string? outputFilePath = commandResult.Children.FirstOrDefault(c => c.Symbol.Name == _outputFilePathOption.Name)?.Tokens[0].Value;
      if (!commandResult.Children.Any(c => c.Symbol.Name == _outputFilePathOption.Name))
      {
        commandResult.ErrorMessage = $"✗ Option '{_outputFilePathOption.Name}' is required";
      }
      else if (outputFilePath != null && string.IsNullOrEmpty(Path.GetFileName(outputFilePath)))
      {
        commandResult.ErrorMessage = $"✗ '{outputFilePath}' is not a valid file path";
      }
    });
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        string publicKey = context.ParseResult.GetValueForArgument(_publicKeyArgument);
        string outputPath = context.ParseResult.GetValueForOption(_outputFilePathOption) ?? throw new KSailException("output path is required");

        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsExportCommandHandler(config, publicKey, outputPath, new SOPSLocalAgeSecretManager());
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddArguments() => AddArgument(_publicKeyArgument);

  void AddOptions() => AddOption(_outputFilePathOption);
}
