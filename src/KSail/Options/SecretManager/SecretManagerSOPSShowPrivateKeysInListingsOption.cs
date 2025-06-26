using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;



class SecretManagerSOPSShowPrivateKeysInListingsOption : Option<bool?>
{
  public SecretManagerSOPSShowPrivateKeysInListingsOption(KSailCluster config) : base(
    "--show-private-keys", "-spk"
  )
  {
    Description = "Show private keys in listings.";
    DefaultValueFactory = (result) => config.Spec.SecretManager.SOPS.ShowPrivateKeysInListings;
  }
}

