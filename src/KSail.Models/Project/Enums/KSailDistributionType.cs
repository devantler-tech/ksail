using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailDistributionType
{
  [Description("Use the native Kubernetes distribution.")]
  Native,

  [Description("Use K3s as the Kubernetes distribution.")]
  K3s
  //
  // Talos
}
