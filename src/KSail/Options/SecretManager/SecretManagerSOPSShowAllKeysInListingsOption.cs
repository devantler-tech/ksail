using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;


class SecretManagerSOPSShowAllKeysInListingsOption(KSailCluster config) : Option<bool?>(
  ["--all", "-a"],
  $"Show all keys. [default: {config.Spec.SecretManager.SOPS.ShowAllKeysInListings}]"
);
