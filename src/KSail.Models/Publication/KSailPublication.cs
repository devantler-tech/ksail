using System.ComponentModel;

namespace KSail.Models.Publication;

public class KSailPublication
{

  [Description("Publish manifests before applying changes to an existing cluster. [default: true]")]
  public bool PublishOnUpdate { get; set; } = true;
}
