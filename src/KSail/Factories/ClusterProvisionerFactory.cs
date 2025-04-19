using System.Runtime.InteropServices;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Factories;

class ClusterProvisionerFactory
{
  internal static IKubernetesClusterProvisioner Create(KSailCluster config)
  {
    switch (config.Spec.Project.Provider)
    {
      case KSailProviderType.Podman:
        string dockerHost = Environment.GetEnvironmentVariable("DOCKER_HOST") ?? "unix:var/run/docker.sock";
        string podmanSocket = RuntimeInformation.IsOSPlatform(OSPlatform.Windows) && File.Exists(@"\\.\pipe\docker_engine") ?
          "npipe://./pipe/docker_engine" : File.Exists($"/run/podman/podman.sock") ?
          $"unix:///run/podman/podman.sock" : File.Exists($"/run/user/{Environment.GetEnvironmentVariable("EUID")}/podman/podman.sock") ?
          $"unix:///run/user/{Environment.GetEnvironmentVariable("EUID")}/podman/podman.sock" : File.Exists($"/run/user/{Environment.GetEnvironmentVariable("UID")}/podman/podman.sock") ?
          $"unix:///run/user/{Environment.GetEnvironmentVariable("UID")}/podman/podman.sock" : dockerHost;
        Environment.SetEnvironmentVariable("DOCKER_HOST", podmanSocket);
        return GetClusterProvisioner(config);
      case KSailProviderType.Docker:
        return GetClusterProvisioner(config);
      default:
        throw new NotSupportedException($"The provider '{config.Spec.Project.Provider}' is not supported.");
    }
  }

  static IKubernetesClusterProvisioner GetClusterProvisioner(KSailCluster config)
  {
    return config.Spec.Project.Distribution switch
    {
      KSailDistributionType.Native => new KindProvisioner(),
      KSailDistributionType.K3s => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{config.Spec.Project.Distribution}' is not supported."),
    };
  }
}
