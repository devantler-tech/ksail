using Devantler.KubernetesProvisioner.GitOps.Flux;
using KSail.Commands.Lint.Handlers;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Commands.Update.Handlers;

class KSailUpdateCommandHandler
{
  readonly FluxProvisioner _deploymentTool;
  readonly KSailCluster _config;
  readonly KSailLintCommandHandler _ksailLintCommandHandler = new();

  internal KSailUpdateCommandHandler(KSailCluster config)
  {
    _deploymentTool = config.Spec.Project.DeploymentTool switch
    {
      KSailDeploymentTool.Flux => new FluxProvisioner(config.Spec.Connection.Context),
      _ => throw new NotSupportedException($"The deployment tool '{config.Spec.Project.DeploymentTool}' is not supported.")
    };
    _config = config;
  }

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    if (!await Lint(_config, cancellationToken).ConfigureAwait(false))
    {
      return false;
    }
    string manifestDirectory = "k8s";
    if (!Directory.Exists(manifestDirectory) || Directory.GetFiles(manifestDirectory, "*.yaml", SearchOption.AllDirectories).Length == 0)
    {
      throw new KSailException($"a '{manifestDirectory}' directory does not exist or is empty.");
    }
    switch (_config.Spec.Project.DeploymentTool)
    {
      case KSailDeploymentTool.Flux:
        string scheme = _config.Spec.FluxDeploymentTool.Source.Url.Scheme;
        string host = "localhost";
        int port = _config.Spec.KSailRegistry.HostPort;
        string absolutePath = _config.Spec.FluxDeploymentTool.Source.Url.AbsolutePath;
        var ociRegistryFromHost = new Uri($"{scheme}://{host}:{port}{absolutePath}");
        Console.WriteLine($"📥 Pushing manifests to '{ociRegistryFromHost}'");
        // TODO: Make some form of abstraction around GitOps tools, so it is easier to support apply-based tools like kubectl
        await _deploymentTool.PushManifestsAsync(ociRegistryFromHost, "k8s", cancellationToken: cancellationToken).ConfigureAwait(false);
        Console.WriteLine();
        if (_config.Spec.CLI.Update.Reconcile)
        {
          Console.WriteLine("🔄 Reconciling changes");
          await _deploymentTool.ReconcileAsync(_config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
        }
        Console.WriteLine();
        break;
      default:
        throw new NotSupportedException($"The deployment tool '{_config.Spec.Project.DeploymentTool}' is not supported.");
    }


    return true;
  }

  async Task<bool> Lint(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.CLI.Update.Lint)
    {
      Console.WriteLine("🔍 Linting manifests");
      bool success = await _ksailLintCommandHandler.HandleAsync(cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
      return success;
    }
    return true;
  }
}
