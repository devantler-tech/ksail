using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;



class SecretManagerSOPSShowPrivateKeysInListingsOption(KSailCluster config) : Option<bool?>(
  ["--show-private-keys", "-spk"],
  $"Show private keys. [default: {config.Spec.SecretManager.SOPS.ShowPrivateKeysInListings}]"
)
{ }
