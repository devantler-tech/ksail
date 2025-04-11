using Devantler.KubernetesProvisioner.Deployment.Core;
using Devantler.KubernetesProvisioner.Deployment.Kubectl;
using Devantler.KubernetesProvisioner.GitOps.Core;
using Devantler.KubernetesProvisioner.GitOps.Flux;
using KSail.Commands.Validate.Handlers;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Update.Handlers;

class KSailUpdateCommandHandler
{
  readonly IDeploymentToolProvisioner _deploymentTool;
  readonly KSailCluster _config;
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler;
  readonly Uri _ociRegistryFromHost;
  internal KSailUpdateCommandHandler(KSailCluster config)
  {
    _ksailValidateCommandHandler = new KSailValidateCommandHandler(config);
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

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    if (!await Validate(_config, cancellationToken).ConfigureAwait(false))
    {
      return false;
    }
    string manifestDirectory = _config.Spec.Project.KustomizationPath
      .Replace("./", string.Empty, StringComparison.OrdinalIgnoreCase)
      .Split('/', StringSplitOptions.RemoveEmptyEntries).First();
    if (!Directory.Exists(manifestDirectory) || Directory.GetFiles(manifestDirectory, "*.yaml", SearchOption.AllDirectories).Length == 0)
    {
      throw new KSailException($"a '{manifestDirectory}' directory does not exist or is empty.");
    }
    switch (_config.Spec.Project.DeploymentTool)
    {
      case KSailDeploymentToolType.Kubectl:
        Console.WriteLine($"🔄 Applying manifests from '{_config.Spec.Project.KustomizationPath}'");
        await _deploymentTool.PushAsync(manifestDirectory, _config.Spec.Connection.Timeout, cancellationToken: cancellationToken).ConfigureAwait(false);
        Console.WriteLine();
        break;
      case KSailDeploymentToolType.Flux:

        Console.WriteLine($"📥 Pushing manifests to '{_ociRegistryFromHost}'");
        // TODO: Make some form of abstraction around GitOps tools, so it is easier to support apply-based tools like kubectl
        await _deploymentTool.PushAsync(manifestDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
        Console.WriteLine();
        if (_config.Spec.Validation.ReconcileOnUpdate)
        {
          Console.WriteLine("🔄 Reconciling changes");
          await ((IGitOpsProvisioner)_deploymentTool).ReconcileAsync(_config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
        }
        Console.WriteLine();
        break;
      default:
        throw new NotSupportedException($"The deployment tool '{_config.Spec.Project.DeploymentTool}' is not supported.");
    }


    return true;
  }

  async Task<bool> Validate(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.ValidateOnUpdate)
    {
      Console.WriteLine("🔍 Validating project files and configuration");
      string kubernetesDirectory = config.Spec.Project.KustomizationPath
        .Replace("./", string.Empty, StringComparison.OrdinalIgnoreCase)
        .Split('/', StringSplitOptions.RemoveEmptyEntries).First();
      bool success = await _ksailValidateCommandHandler.HandleAsync(kubernetesDirectory, cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
      return success;
    }
    return true;
  }
}
