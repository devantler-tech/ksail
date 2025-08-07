using System.CommandLine;
using System.Globalization;
using DevantlerTech.KubernetesProvisioner.Deployment.Core;
using DevantlerTech.KubernetesProvisioner.Deployment.Kubectl;
using DevantlerTech.KubernetesProvisioner.GitOps.Core;
using DevantlerTech.KubernetesProvisioner.GitOps.Flux;
using KSail.Commands.Validate.Handlers;
using KSail.Managers;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Update.Handlers;

class KSailUpdateCommandHandler : ICommandHandler
{
  readonly IDeploymentToolProvisioner _deploymentTool;
  readonly KSailCluster _config;
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler;
  readonly Uri _ociRegistryFromHost;
  internal KSailUpdateCommandHandler(KSailCluster config)
  {
    _ksailValidateCommandHandler = new KSailValidateCommandHandler(config, "./");
    string scheme = config.Spec.DeploymentTool.Flux.Source.Url.Scheme;
    string host = "localhost";
    int port = config.Spec.LocalRegistry.HostPort;
    string absolutePath = config.Spec.DeploymentTool.Flux.Source.Url.AbsolutePath;
    _ociRegistryFromHost = new Uri($"{scheme}://{host}:{port}{absolutePath}");
    _deploymentTool = config.Spec.Project.DeploymentTool switch
    {
      KSailDeploymentToolType.Kubectl => new KubectlProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      KSailDeploymentToolType.Flux => new FluxProvisioner(_ociRegistryFromHost, config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      _ => throw new NotSupportedException($"The deployment tool '{config.Spec.Project.DeploymentTool}' is not supported.")
    };
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
        KSailDeploymentToolType.Flux => $"📥 Pushing manifests to '{_ociRegistryFromHost}'",
        _ => throw new NotSupportedException($"The deployment tool '{_config.Spec.Project.DeploymentTool}' is not supported.")
      });
      await _deploymentTool.PushAsync(manifestDirectory, _config.Spec.Connection.Timeout, cancellationToken: cancellationToken).ConfigureAwait(false);
    }

    if (_config.Spec.Validation.ReconcileOnUpdate)
    {
      Console.WriteLine();
      string kubernetesDirectory = manifestDirectory;

      // Use enhanced reconciliation progress manager for Flux
      if (_deploymentTool is IGitOpsProvisioner)
      {
        using var progressManager = new ReconciliationProgressManager(
          _config.Spec.Connection.Kubeconfig,
          _config.Spec.Connection.Context,
          TimeSpan.Parse(_config.Spec.Connection.Timeout, CultureInfo.InvariantCulture));

        await progressManager.ReconcileWithProgressAsync(_deploymentTool, kubernetesDirectory, cancellationToken).ConfigureAwait(false);
      }
      else
      {
        // Fallback to original behavior for non-GitOps provisioners
        Console.WriteLine("🔄 Reconciling changes...");
        await _deploymentTool.ReconcileAsync(manifestDirectory, _config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
        Console.WriteLine("✔ reconciliation completed");
      }
    }
  }

  async Task<bool> Validate(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.ValidateOnUpdate)
    {
      await _ksailValidateCommandHandler.HandleAsync(cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
    }
    return true;
  }
}
