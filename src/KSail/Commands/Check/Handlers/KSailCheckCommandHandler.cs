using System.Diagnostics;
using k8s;
using k8s.Models;
using KSail.Extensions;

namespace KSail.Commands.Check.Handlers;

class KSailCheckCommandHandler()
{
  readonly HashSet<string> _kustomizations = [];
  readonly HashSet<string> _successFullKustomizations = [];
  readonly Stopwatch _stopwatch = Stopwatch.StartNew();

  internal async Task<int> HandleAsync(string context, int timeout, CancellationToken token, string? kubeconfig = null)
  {
    Console.WriteLine("👀 Checking the status of the cluster...");
    var kubernetesClient = (kubeconfig is not null) switch
    {
      true => new Kubernetes(KubernetesClientConfiguration.BuildConfigFromConfigFile(kubeconfig)),
      false => CreateKubernetesClientFromClusterName(context)
    };
    var responseTask = kubernetesClient.ListKustomizationsWithHttpMessagesAsync();

    await foreach (var (type, kustomization) in responseTask.WatchAsync<V1CustomResourceDefinition, object>(cancellationToken: token))
    {
      string? kustomizationName = kustomization?.Metadata.Name ??
        throw new InvalidOperationException("🚨 Kustomization name is null");
      string? statusConditionStatus = kustomization?.Status.Conditions.FirstOrDefault()?.Status ??
        throw new InvalidOperationException("🚨 Kustomization status is null");
      string? statusConditionType = kustomization?.Status.Conditions.FirstOrDefault()?.Type ??
        throw new InvalidOperationException("🚨 Kustomization status is null");

      if (!_kustomizations.Add(kustomizationName))
      {
        if (_successFullKustomizations.Count == _kustomizations.Count)
        {
          Console.WriteLine("✔ All kustomizations are ready!");
          return 0;
        }
        else if (_successFullKustomizations.Contains(kustomizationName))
        {
          continue;
        }
        else if (_stopwatch.Elapsed.TotalSeconds >= timeout)
        {
          Console.WriteLine($"✕ Kustomization '{kustomizationName}' did not become ready within the specified time limit of {timeout} seconds.");
          return 1;
        }
      }
      if (statusConditionStatus.Equals("false", StringComparison.OrdinalIgnoreCase))
      {
        continue;
      }
      switch (statusConditionType)
      {
        //TODO: Implement check command with condition[1].type == healthy. This should work for all kustomizations.
        case "Failed":
          return HandleFailedStatus(kustomization, kustomizationName);
        case "Ready":
          HandleReadyStatus(kustomizationName);
          break;
        default:
          Console.WriteLine($"◎ Waiting for kustomization '{kustomizationName}' to be ready...");
          Console.WriteLine($"  Current status: {statusConditionType}");
          foreach (var condition in kustomization?.Status.Conditions ?? Enumerable.Empty<V1CustomResourceDefinitionCondition>())
          {
            Console.WriteLine($"  {condition.Message}");
          }
          Console.WriteLine($"  Elapsed time: {_stopwatch.Elapsed.TotalSeconds:0}s out of {timeout}s");
          break;
      }
    }
    return 0;
  }

  void HandleReadyStatus(string kustomizationName)
  {
    Console.WriteLine($"✔ Kustomization '{kustomizationName}' is ready! Resetting timer...");
    _ = _successFullKustomizations.Add(kustomizationName);
    _stopwatch.Restart();
  }

  static int HandleFailedStatus(V1CustomResourceDefinition? kustomization, string kustomizationName)
  {
    string? message = kustomization?.Status.Conditions.FirstOrDefault()?.Message;
    Console.WriteLine($"✕ Kustomization '{kustomizationName}' failed with message: {message}");
    return 1;
  }

  static Kubernetes CreateKubernetesClientFromClusterName(string context)
  {
    var kubeConfig = KubernetesClientConfiguration.LoadKubeConfig();
    var config = KubernetesClientConfiguration.BuildConfigFromConfigObject(kubeConfig, context);
    return new Kubernetes(config);
  }
}
