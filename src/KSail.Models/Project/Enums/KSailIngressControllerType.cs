using System.ComponentModel;

namespace KSail.Models.Project.Enums;

public enum KSailIngressControllerType
{
  [Description("Use the default Ingress Controller that comes with the chosen Kubernetes distribution.")]
  Default
}
