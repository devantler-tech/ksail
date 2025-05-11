using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Publication;

class PublicationPublishOnUpdateOption(KSailCluster config) : Option<bool?>(
  ["--publish", "-p"],
  $"Publish manifests. [default: {config.Spec.Publication.PublishOnUpdate}]"
);

