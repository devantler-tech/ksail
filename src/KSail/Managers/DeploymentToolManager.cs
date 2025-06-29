using DevantlerTech.KubernetesProvisioner.Deployment.Core;
using DevantlerTech.KubernetesProvisioner.GitOps.Core;
using DevantlerTech.KubernetesProvisioner.Resources.Native;
using KSail.Commands.Up.Extensions;
using KSail.Factories;
using KSail.Models;

namespace KSail.Managers;

class DeploymentToolManager(KSailCluster config) : IBootstrapManager
{
  readonly IDeploymentToolProvisioner _deploymentToolProvisioner = DeploymentToolProvisionerFactory.Create(config);
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🚀 Bootstrapping {config.Spec.Project.DeploymentTool}");
    string kubernetesDirectory = config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First();
    await _deploymentToolProvisioner.PushAsync(kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    if (_deploymentToolProvisioner is IGitOpsProvisioner gitOpsProvisioner)
    {
      Console.WriteLine($"► creating 'flux-system' namespace");
      using var kubernetesResourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context);
      await kubernetesResourceProvisioner.CreateNamespaceAsync("flux-system", cancellationToken).ConfigureAwait(false);
      string ociKustomizationPath = config.Spec.Project.KustomizationPath[kubernetesDirectory.Length..].TrimStart('/');
      await gitOpsProvisioner.InstallAsync(
        config.Spec.DeploymentTool.Flux.Source.Url,
        ociKustomizationPath,
        true,
        cancellationToken
      ).ConfigureAwait(false);
    }
    if (config.Spec.Validation.ReconcileOnUp)
      await ReconcileAsync(cancellationToken).ConfigureAwait(false);
  }
  async Task ReconcileAsync(CancellationToken cancellationToken)
  {
    Console.WriteLine();
    Console.WriteLine("🔄 Reconciling changes...");
    string kubernetesDirectory = config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First();
    await _deploymentToolProvisioner.ReconcileAsync(kubernetesDirectory, config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ reconciliation completed");
  }
}
