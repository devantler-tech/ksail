using Devantler.SecretManager.SOPS.LocalAge.Models;
using Devantler.SecretManager.SOPS.LocalAge.Utils;

namespace KSail.Commands.Gen.Handlers.Config;

class KSailGenConfigSOPSCommandHandler
{
  readonly SOPSConfigHelper _configHelper = new();
  internal async Task<int> HandleAsync(string outputFile, CancellationToken cancellationToken = default)
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
    await _configHelper.CreateSOPSConfigAsync(outputFile, sopsConfig, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
