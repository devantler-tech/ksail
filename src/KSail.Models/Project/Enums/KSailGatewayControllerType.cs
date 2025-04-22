using System.ComponentModel;

namespace KSail.Models.Project.Enums;

public enum KSailGatewayControllerType
{
  [Description("Do not use a Gateway Controller.")]
  None,
  [Description("Use the default Gateway Controller that comes with the chosen Kubernetes distribution.")]
  Default
}
