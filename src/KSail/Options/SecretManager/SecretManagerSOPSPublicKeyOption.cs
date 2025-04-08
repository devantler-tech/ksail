using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;


class SecretManagerSOPSPublicKeyOption(KSailCluster cluster) : Option<string?>(
  ["--public-key", "-pk"],
  $"The public key. [default: {cluster.Spec.SecretManager.SOPS.PublicKey}]"
);
