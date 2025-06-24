using DevantlerTech.SecretManager.SOPS.LocalAge.Models;
using DevantlerTech.SecretManager.SOPS.LocalAge.Utils;

namespace KSail.Commands.Gen.Handlers.Config;

class KSailGenConfigSOPSCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly SOPSConfigHelper _configHelper = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var sopsConfig = new SOPSConfig
    {
      CreationRules =
      [
        new() {
          PathRegex = @"^.+\.enc\.ya?ml$",
          EncryptedRegex = "^(data|stringData)$",
          Age = """
          <age-public-key-1>
          """,
        }
      ]
    };
    await _configHelper.CreateSOPSConfigAsync(outputFile, sopsConfig, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
