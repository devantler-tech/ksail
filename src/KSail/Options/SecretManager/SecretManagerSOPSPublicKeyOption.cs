using System.CommandLine;

namespace KSail.Options.SecretManager;


class SecretManagerSOPSPublicKeyOption() : Option<string?>(
  ["--public-key", "-pk"],
  $"The public key."
)
{
}
