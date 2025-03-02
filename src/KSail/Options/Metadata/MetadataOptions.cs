using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Metadata;



class MetadataOptions(KSailCluster config)
{

  public readonly MetadataNameOption NameOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
}
