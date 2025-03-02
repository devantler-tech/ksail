using KSail.Models;

namespace KSail.Options.SecretManager;



class SecretManagerOptions(KSailCluster config)
{

  public readonly SecretManagerSOPSOptions SOPS = new(config);
}
