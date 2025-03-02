using System.CommandLine;
using KSail.Models;

namespace KSail.Options.SecretManager;



class SecretManagerSOPSOptions(KSailCluster config)
{

  public readonly SecretManagerSOPSPublicKeyOption PublicKeyOption = new() { Arity = ArgumentArity.ZeroOrOne };


  public readonly SecretManagerSOPSInPlaceOption InPlaceOption = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public readonly SecretManagerSOPSShowPrivateKeysInListingsOption ShowPrivateKeysInListingsOption = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public readonly SecretManagerSOPSShowAllKeysInListingsOption ShowAllKeysInListingsOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
}
