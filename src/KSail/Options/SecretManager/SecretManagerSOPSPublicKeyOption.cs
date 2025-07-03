using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;


class SecretManagerSOPSPublicKeyOption : Option<string?>
{
  public SecretManagerSOPSPublicKeyOption(KSailCluster cluster) : base(
    "--public-key", "-pk"
  )
  {
    Description = "The public key to use.";
    DefaultValueFactory = (result) => cluster.Spec.SecretManager.SOPS.PublicKey;
  }
}

