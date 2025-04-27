using Devantler.KubernetesProvisioner.Deployment.Core;
using Devantler.KubernetesProvisioner.Deployment.Kubectl;
using Devantler.KubernetesProvisioner.GitOps.ArgoCD;
using Devantler.KubernetesProvisioner.GitOps.Core;
using Devantler.KubernetesProvisioner.GitOps.Flux;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Update.Handlers;

class KSailUpdateCommandHandler(KSailCluster config)
{
  readonly IDeploymentToolProvisioner _deploymentTool = DeploymentToolProvisionerFactory.Create(config);
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler = new(config);

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    if (!await Validate(cancellationToken).ConfigureAwait(false))
    {
      return false;
    }
    string manifestDirectory = config.Spec.Project.KustomizationPath
      .Replace("./", string.Empty, StringComparison.OrdinalIgnoreCase)
      .Split('/', StringSplitOptions.RemoveEmptyEntries).First();
    if (!Directory.Exists(manifestDirectory) || Directory.GetFiles(manifestDirectory, "*.yaml", SearchOption.AllDirectories).Length == 0)
    {
      throw new KSailException($"a '{manifestDirectory}' directory does not exist or is empty.");
    }
    Console.WriteLine(config.Spec.Project.DeploymentTool switch
    {
      KSailDeploymentToolType.Kubectl => $"⤴️ Applying manifests from '{config.Spec.Project.KustomizationPath}'",
      KSailDeploymentToolType.Flux or KSailDeploymentToolType.ArgoCD => $"📥 Pushing manifests to '{new Uri($"{config.Spec.DeploymentTool.Flux.Source.Url.Scheme}://localhost:{config.Spec.LocalRegistry.HostPort}{config.Spec.DeploymentTool.Flux.Source.Url.AbsolutePath}")}'",
      _ => throw new NotSupportedException($"The deployment tool '{config.Spec.Project.DeploymentTool}' is not supported.")
    });
    await _deploymentTool.PushAsync(manifestDirectory, config.Spec.Connection.Timeout, cancellationToken: cancellationToken).ConfigureAwait(false);

    if (config.Spec.Validation.ReconcileOnUpdate)
    {
      Console.WriteLine();
      Console.WriteLine("🔄 Reconciling changes...");
      await _deploymentTool.ReconcileAsync(manifestDirectory, config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
      Console.WriteLine("✔ reconciliation completed");
    }

    return true;
  }

  async Task<bool> Validate(CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.ValidateOnUpdate)
    {
      bool success = await _ksailValidateCommandHandler.HandleAsync("./", cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
      return success;
    }
    return true;
  }
}
