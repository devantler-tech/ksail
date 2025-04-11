using System.ComponentModel;

namespace KSail.Models.SecretManager;


public class KSailSecretManagerSOPS
{

  [Description("Public key used for encryption. [default: null]")]
  public string PublicKey { get; set; } = string.Empty;


  [Description("Use in-place decryption/encryption. [default: false]")]
  public bool InPlace { get; set; }


  [Description("Show all keys in the listed keys. [default: false]")]
  public bool ShowAllKeysInListings { get; set; }


  [Description("Show private keys in the listed keys. [default: false]")]
  public bool ShowPrivateKeysInListings { get; set; }
}
