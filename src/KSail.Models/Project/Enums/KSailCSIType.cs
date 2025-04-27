using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailCSIType
{
  [Description("Use the default CSI that comes with the chosen Kubernetes distribution.")]
  Default,
  [Description("Use the Local Path Provisioner CSI.")]
  LocalPathProvisioner,
  [Description("Do not use a CSI.")]
  None
}
