using System.ComponentModel;

namespace KSail.Models.Project.Enums;

public enum KSailIngressControllerType
{
  [Description("Use the default Ingress Controller that comes with the chosen Kubernetes distribution.")]
  Default,
  [Description("Use Traefik as the Ingress Controller.")]
  Traefik,
  [Description("Do not use an Ingress Controller.")]
  None
}
