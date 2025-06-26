using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;



class SecretManagerSOPSInPlaceOption : Option<bool?>
{
  public SecretManagerSOPSInPlaceOption(KSailCluster config) : base(
    "--in-place", "-ip"
  )
  {
    Description = "In-place decryption/encryption.";
    DefaultValueFactory = (result) => config.Spec.SecretManager.SOPS.InPlace;
  }
}

