using System.CommandLine;
using KSail.Models;
using KSail.Options.Validation;

namespace KSail.Options.Publication;

class PublicationOptions(KSailCluster config)
{
  public PublicationPublishOnUpdateOption PublishOnUpdateOption { get; } = new(config)
  {
    Arity = ArgumentArity.ZeroOrOne
  };
}
