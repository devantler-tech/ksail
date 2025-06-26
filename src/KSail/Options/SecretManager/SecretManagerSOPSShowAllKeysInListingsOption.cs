using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;


class SecretManagerSOPSShowAllKeysInListingsOption : Option<bool?>
{
  public SecretManagerSOPSShowAllKeysInListingsOption(KSailCluster config) : base(
    "--all", "-a"
  )
  {
    Description = "Show all keys in listings.";
    DefaultValueFactory = (result) => config.Spec.SecretManager.SOPS.ShowAllKeysInListings;
  }
}

