using System.ComponentModel;

namespace KSail.Models.Project.Enums;

public enum KSailCNIType
{
  [Description("Use the default CNI that comes with the chosen Kubernetes distribution.")]
  Default,

  [Description("Use Cilium as the CNI.")]
  Cilium,
}
