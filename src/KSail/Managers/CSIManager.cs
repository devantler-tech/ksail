using System.Threading.Tasks;
using Devantler.KubernetesProvisioner.Resources.Native;
using k8s;
using KSail;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class CSIManager(KSailCluster config) : IBootstrapManager
{

  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("💾 Bootstrapping CSI");
    switch (config.Spec.Project.CSI)
    {
      case KSailCSIType.Default:
        HandleDefaultCSI();
        break;
      case KSailCSIType.LocalPathProvisioner:
        HandleLocalPathProvisionerCSI();
        break;
      case KSailCSIType.None:
        await HandleNoCSIAsync(cancellationToken).ConfigureAwait(false);
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
    Console.WriteLine();
  }

  async Task HandleNoCSIAsync(CancellationToken cancellationToken = default)
  {
    using var kubernetesResourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context);
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
      case KSailDistributionType.K3d:
        Console.WriteLine("► Removing the local-path-provisioner CSI");
        _ = await kubernetesResourceProvisioner.DeleteStorageClassAsync("local-path", cancellationToken: cancellationToken).ConfigureAwait(false);
        _ = await kubernetesResourceProvisioner.DeleteClusterRoleBindingAsync("local-path-provisioner-bind", cancellationToken: cancellationToken).ConfigureAwait(false);
        _ = await kubernetesResourceProvisioner.DeleteClusterRoleAsync("local-path-provisioner-role", cancellationToken: cancellationToken).ConfigureAwait(false);
        _ = await kubernetesResourceProvisioner.DeleteNamespaceAsync("local-path-storage", cancellationToken: cancellationToken).ConfigureAwait(false);
        Console.WriteLine("✔ local-path-provisioner CSI removed.");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
  }

  void HandleLocalPathProvisionerCSI()
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("✔ Kind deploys the local-path-provisioner CSI by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ K3d deploys the local-path-provisioner CSI by default");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
  }

  void HandleDefaultCSI()
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Kind deploys the local-path-provisioner CSI by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("► K3d deploys the local-path-provisioner CSI by default");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
  }
}
