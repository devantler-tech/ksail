using System.ComponentModel;

namespace KSail.Models.DeploymentTool;


/// <remarks>
/// Constructs a new instance of the KSail Flux Deployment Tool Repository.
/// </remarks>
public class KSailFluxDeploymentToolRepository
{

  [Description("The URL of the repository. [default: oci://host.docker.internal:5555/ksail-registry]")]
  public Uri Url { get; set; } = new Uri("oci://host.docker.internal:5555/ksail-registry");
}
