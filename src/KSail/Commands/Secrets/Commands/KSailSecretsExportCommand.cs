using System.CommandLine;
using DevantlerTech.SecretManager.SOPS.LocalAge;
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
  readonly GenericPathOption _outputFilePathOption = new("--output", ["-o"], string.Empty) { Arity = ArgumentArity.ExactlyOne };
  internal KSailSecretsExportCommand() : base("export", "Export a key to a file")
  {
    AddArguments();
    AddOptions();

    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        string publicKey = parseResult.GetValue(_publicKeyArgument) ?? throw new KSailException("public key is required");
        string outputPath = parseResult.GetValue(_outputFilePathOption) ?? throw new KSailException("output path is required");

        var handler = new KSailSecretsExportCommandHandler(publicKey, outputPath, new SOPSLocalAgeSecretManager());
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

  void AddArguments() => Arguments.Add(_publicKeyArgument);

  void AddOptions() => Options.Add(_outputFilePathOption);
}
