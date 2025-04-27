using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailDistributionType
{
  [Description("Use the Kind Kubernetes distribution.")]
  Kind,

  [Description("Use the K3d Kubernetes distribution.")]
  K3d
  //
  // Talos
}
