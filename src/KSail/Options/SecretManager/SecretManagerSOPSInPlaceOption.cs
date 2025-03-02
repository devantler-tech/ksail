using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;



class SecretManagerSOPSInPlaceOption(KSailCluster config) : Option<bool>(
  ["--in-place", "-ip"],
  $"In-place decryption/encryption. [default: {config.Spec.SecretManager.SOPS.InPlace}]"
)
{
}
