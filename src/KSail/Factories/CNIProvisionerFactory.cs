using Devantler.KubernetesProvisioner.CNI.Cilium;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Factories;

class CNIProvisionerFactory
{
  internal static CiliumProvisioner? Create(KSailCluster config)
  {
    return config.Spec.Project.CNI switch
    {
      KSailCNIType.Cilium => new CiliumProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      KSailCNIType.Default => null,
      KSailCNIType.None => null,
      _ => throw new NotSupportedException($"The CNI '{config.Spec.Project.CNI}' is not supported.")
    };
  }
}
