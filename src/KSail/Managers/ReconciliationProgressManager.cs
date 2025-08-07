using System.Globalization;
using DevantlerTech.KubernetesProvisioner.Deployment.Core;
using k8s;
using k8s.Models;
using System.Text.Json;

namespace KSail.Managers;

/// <summary>
/// Provides enhanced reconciliation progress tracking for Flux kustomizations
/// </summary>
class ReconciliationProgressManager : IDisposable
{
  readonly Kubernetes _kubernetesClient;
  readonly string? _context;
  readonly TimeSpan _timeout;

  public ReconciliationProgressManager(string kubeconfig, string? context, TimeSpan timeout)
  {
    _context = context;
    _timeout = timeout;

    var config = string.IsNullOrEmpty(kubeconfig)
      ? KubernetesClientConfiguration.InClusterConfig()
      : KubernetesClientConfiguration.BuildConfigFromConfigFile(kubeconfig, _context);

    _kubernetesClient = new Kubernetes(config);
  }

  /// <summary>
  /// Wraps the deployment tool reconciliation with enhanced progress reporting
  /// </summary>
  public async Task ReconcileWithProgressAsync(IDeploymentToolProvisioner deploymentTool, string manifestDirectory, CancellationToken cancellationToken)
  {
    Console.WriteLine("🔄 Starting reconciliation with enhanced progress tracking...");

    // Start the actual reconciliation in the background
    var reconcileTask = deploymentTool.ReconcileAsync(manifestDirectory, _timeout.ToString(), cancellationToken);

    // Start progress monitoring
    var progressTask = MonitorReconciliationProgressAsync(cancellationToken);

    // Wait for both to complete
    await Task.WhenAll(reconcileTask, progressTask).ConfigureAwait(false);

    Console.WriteLine("✔ reconciliation completed");
  }

  /// <summary>
  /// Monitors and reports reconciliation progress
  /// </summary>
  async Task MonitorReconciliationProgressAsync(CancellationToken cancellationToken)
  {
    var startTime = DateTime.UtcNow;
    var reported = new HashSet<string>();

    try
    {
      while (!cancellationToken.IsCancellationRequested)
      {
        var kustomizations = await GetFluxKustomizationsAsync(cancellationToken).ConfigureAwait(false);

        if (kustomizations?.Items?.Count > 0)
        {
          bool allReady = true;
          var statusReport = new List<string>();

          foreach (var kustomization in kustomizations.Items)
          {
            string name = kustomization.Metadata?.Name ?? "unknown";
            string ns = kustomization.Metadata?.NamespaceProperty ?? "default";
            string key = $"{ns}/{name}";

            var status = GetKustomizationStatus(kustomization);
            double remainingTimeout = CalculateRemainingTimeout(startTime);

            if (!status.IsReady)
            {
              allReady = false;

              if (!reported.Contains(key) || ShouldUpdateStatus())
              {
                if (status.WaitingForDependency != null)
                {
                  statusReport.Add($"  '{name}' waiting for '{status.WaitingForDependency}'. Timeout in {remainingTimeout:F2} seconds.");
                }
                else
                {
                  statusReport.Add($"  '{name}' reconciling... Timeout in {remainingTimeout:F2} seconds.");
                }
                _ = reported.Add(key);
              }
            }
            else if (!reported.Contains($"{key}-ready"))
            {
              statusReport.Add($"  ✔ '{name}' reconciliation completed");
              _ = reported.Add($"{key}-ready");
            }
          }

          if (statusReport.Count > 0)
          {
            Console.WriteLine("◎ Kustomization reconciliation progress:");
            foreach (string report in statusReport)
            {
              Console.WriteLine(report);
            }
          }

          if (allReady)
          {
            break;
          }
        }

        await Task.Delay(2000, cancellationToken).ConfigureAwait(false); // Check every 2 seconds
      }
    }
    catch (Exception ex) when (ex is not OperationCanceledException)
    {
      // Log error but don't break reconciliation
      Console.WriteLine($"⚠️ Progress monitoring error: {ex.Message}");
    }
  }

  /// <summary>
  /// Gets Flux kustomizations from the cluster
  /// </summary>
  async Task<V1CustomResourceList<FluxKustomization>?> GetFluxKustomizationsAsync(CancellationToken cancellationToken)
  {
    try
    {
      object response = await _kubernetesClient.CustomObjects.ListClusterCustomObjectAsync(
        group: "kustomize.toolkit.fluxcd.io",
        version: "v1",
        plural: "kustomizations",
        cancellationToken: cancellationToken).ConfigureAwait(false);

      if (response is JsonElement jsonElement)
      {
        string json = jsonElement.GetRawText();
        return JsonSerializer.Deserialize<V1CustomResourceList<FluxKustomization>>(json);
      }
    }
    catch (Exception ex)
    {
      // Silently handle errors - cluster might not have Flux installed yet
      Console.WriteLine($"⚠️ Unable to query Flux kustomizations: {ex.Message}");
    }

    return null;
  }

  /// <summary>
  /// Determines the status of a kustomization
  /// </summary>
  static KustomizationStatus GetKustomizationStatus(FluxKustomization kustomization)
  {
    var conditions = kustomization.Status?.Conditions ?? [];
    var readyCondition = conditions.FirstOrDefault(c => c.Type == "Ready");

    bool isReady = readyCondition?.Status == "True";
    string? waitingFor = null;

    if (!isReady)
    {
      // Check if waiting for dependency
      var dependsOnCondition = conditions.FirstOrDefault(c => c.Type == "DependencyNotReady");
      if (dependsOnCondition?.Status == "True")
      {
        waitingFor = ExtractDependencyName(dependsOnCondition.Message);
      }
    }

    return new KustomizationStatus(isReady, waitingFor);
  }

  /// <summary>
  /// Extracts dependency name from condition message
  /// </summary>
  static string? ExtractDependencyName(string? message)
  {
    if (string.IsNullOrEmpty(message)) return null;

    // Try to extract dependency name from message like "dependency 'infrastructure' is not ready"
    string[] parts = message.Split('\'');
    return parts.Length >= 2 ? parts[1] : null;
  }

  /// <summary>
  /// Calculates remaining timeout
  /// </summary>
  double CalculateRemainingTimeout(DateTime startTime)
  {
    var elapsed = DateTime.UtcNow - startTime;
    var remaining = _timeout - elapsed;
    return Math.Max(0, remaining.TotalSeconds);
  }

  /// <summary>
  /// Determines if status should be updated (for periodic updates)
  /// </summary>
  static bool ShouldUpdateStatus() =>
    // Update every 30 seconds or when conditions change
    DateTime.UtcNow.Second % 30 == 0;

  public void Dispose() => _kubernetesClient?.Dispose();
}

/// <summary>
/// Represents the status of a Flux kustomization
/// </summary>
record KustomizationStatus(bool IsReady, string? WaitingForDependency);

/// <summary>
/// Flux Kustomization resource model
/// </summary>
class FluxKustomization
{
  public V1ObjectMeta? Metadata { get; set; }
  public FluxKustomizationStatus? Status { get; set; }
}

/// <summary>
/// Flux Kustomization status model
/// </summary>
class FluxKustomizationStatus
{
  public List<FluxCondition>? Conditions { get; set; }
}

/// <summary>
/// Flux condition model
/// </summary>
class FluxCondition
{
  public string? Type { get; set; }
  public string? Status { get; set; }
  public string? Message { get; set; }
}

/// <summary>
/// Custom resource list for Flux kustomizations
/// </summary>
class V1CustomResourceList<T>
{
  public List<T>? Items { get; set; }
}