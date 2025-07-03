using System.CommandLine;
using DevantlerTech.KubernetesProvisioner.Deployment.Core;
using DevantlerTech.KubernetesProvisioner.Deployment.Kubectl;
using DevantlerTech.KubernetesProvisioner.GitOps.ArgoCD;
using DevantlerTech.KubernetesProvisioner.GitOps.Core;
using DevantlerTech.KubernetesProvisioner.GitOps.Flux;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Update.Handlers;

class KSailUpdateCommandHandler : ICommandHandler
{
  readonly IDeploymentToolProvisioner _deploymentTool;
  readonly KSailCluster _config;
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler;
  internal KSailUpdateCommandHandler(KSailCluster config)
  {
    _ksailValidateCommandHandler = new KSailValidateCommandHandler(config, "./");
    _deploymentTool = DeploymentToolProvisionerFactory.Create(config);
    _config = config;
  }

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    _ = await Validate(_config, cancellationToken).ConfigureAwait(false);
    string manifestDirectory = _config.Spec.Project.KustomizationPath
      .Replace("./", string.Empty, StringComparison.OrdinalIgnoreCase)
      .Split('/', StringSplitOptions.RemoveEmptyEntries).First();
    if (_config.Spec.Publication.PublishOnUpdate)
    {
      if (!Directory.Exists(manifestDirectory) || Directory.GetFiles(manifestDirectory, "*.yaml", SearchOption.AllDirectories).Length == 0)
      {
        throw new KSailException($"a '{manifestDirectory}' directory does not exist or is empty.");
      }
      Console.WriteLine(_config.Spec.Project.DeploymentTool switch
      {
        KSailDeploymentToolType.Kubectl => $"⤴️ Applying manifests from '{_config.Spec.Project.KustomizationPath}'",
        KSailDeploymentToolType.Flux or KSailDeploymentToolType.ArgoCD => $"📥 Pushing manifests to 'ksail-registry'",
        _ => throw new NotSupportedException($"The deployment tool '{_config.Spec.Project.DeploymentTool}' is not supported.")
      });
      await _deploymentTool.PushAsync(manifestDirectory, _config.Spec.Connection.Timeout, cancellationToken: cancellationToken).ConfigureAwait(false);
    }

    if (_config.Spec.Validation.ReconcileOnUpdate)
    {
      Console.WriteLine();
      Console.WriteLine("🔄 Reconciling changes...");
      await _deploymentTool.ReconcileAsync(manifestDirectory, _config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
      Console.WriteLine("✔ reconciliation completed");
    }
  }

  async Task<bool> Validate(CancellationToken cancellationToken = default)
  {
    if (_config.Spec.Validation.ValidateOnUpdate)
    {
      await _ksailValidateCommandHandler.HandleAsync(cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
    }
    return true;
  }
}
