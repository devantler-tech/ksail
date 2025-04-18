using System.ComponentModel;

namespace KSail.Models.DeploymentTool;


/// <remarks>
/// Constructs a new instance of the KSail Flux Deployment Tool Repository.
/// </remarks>
public class KSailFluxDeploymentToolRepository
{
  [Description("The URL of the repository. [default: oci://ksail-registry:5000/ksail-registry]")]
  public Uri Url { get; set; } = new Uri("oci://ksail-registry:5000/ksail-registry");
}
