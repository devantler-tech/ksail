using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Publication;

class PublicationPublishOnUpdateOption : Option<bool?>
{
  public PublicationPublishOnUpdateOption(KSailCluster config) : base(
    "--publish", "-p"
  )
  {
    Description = "Whether to publish manifests on update.";
    DefaultValueFactory = (result) => config.Spec.Publication.PublishOnUpdate;
  }
}


