
using Devantler.KubernetesGenerator.Kustomize;
using Devantler.KubernetesGenerator.Kustomize.Models;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Commands.Init.Generators.SubGenerators;

class KustomizeFlowGenerator
{
  readonly KustomizeKustomizationGenerator _kustomizationGenerator = new();
  internal async Task GenerateAsync(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.KustomizeTemplate.Hooks.Length == 0)
    {
      config.Spec.KustomizeTemplate.Hooks = [""];
    }
    foreach (string currentHook in config.Spec.KustomizeTemplate.Hooks)
    {
      foreach (string currentFlow in config.Spec.KustomizeTemplate.Flows)
      {
        await GenerateKustomizeFlowHook(config, currentHook, currentFlow, cancellationToken).ConfigureAwait(false);
      }
    }
    if (config.Spec.FluxDeploymentTool.PostBuildVariables)
    {
      foreach (string hook in config.Spec.KustomizeTemplate.Hooks)
      {
        await GenerateKustomizeFlowHook(config, hook, "variables", cancellationToken).ConfigureAwait(false);
      }
    }
  }

  async Task GenerateKustomizeFlowHook(KSailCluster config, string currentHook, string currentFlow, CancellationToken cancellationToken = default)
  {
    string outputPath = Path.Combine(config.Spec.Project.WorkingDirectory, "k8s", currentHook, currentFlow);
    if (!Directory.Exists(outputPath))
      _ = Directory.CreateDirectory(outputPath);
    string outputDirectory = Path.Combine(outputPath, "kustomization.yaml");
    if (File.Exists(outputDirectory))
    {
      Console.WriteLine($"✔ skipping '{outputDirectory}', as it already exists.");
      return;
    }
    Console.WriteLine($"✚ generating '{outputDirectory}'");
    string relativeRoot = string.Join("/", Enumerable.Repeat("..", Path.Combine(currentHook, currentFlow).Split('/').Length));
    KustomizeKustomization? kustomization;

    List<string>? components = null;
    if (config.Spec.KustomizeTemplate.Components && currentHook == config.Spec.KustomizeTemplate.Hooks.First())
    {
      components = [];
      if (config.Spec.Project.DeploymentTool == KSailDeploymentTool.Flux)
      {
        components.Add(Path.Combine(relativeRoot, "components/helm-release-crds-label"));
        components.Add(Path.Combine(relativeRoot, "components/helm-release-remediation-label"));
      }
    }

    if (currentFlow == "variables")
    {
      string pathToNextFlow = currentHook == config.Spec.KustomizeTemplate.Hooks.Last() ?
        "" :
        Path.Combine(
          relativeRoot,
          $"{config.Spec.KustomizeTemplate.Hooks.ElementAt(
            Array.IndexOf([.. config.Spec.KustomizeTemplate.Hooks], currentHook) + 1
          )}/{currentFlow}"
        );
      kustomization = new KustomizeKustomization
      {
        Resources = [
          Path.Combine($"variables.yaml"),
          Path.Combine($"variables-sensitive.sops.yaml")
        ],
        Components = components
      };
      if (!string.IsNullOrEmpty(pathToNextFlow))
        kustomization.Resources = kustomization.Resources.Prepend(pathToNextFlow);
    }
    else
    {
      kustomization = currentHook == config.Spec.KustomizeTemplate.Hooks.Last()
        ? new KustomizeKustomization
        {
          Resources = [],
          Components = components
        }
        : new KustomizeKustomization
        {
          Resources = [Path.Combine(relativeRoot, $"{config.Spec.KustomizeTemplate.Hooks.ElementAt(Array.IndexOf([.. config.Spec.KustomizeTemplate.Hooks], currentHook) + 1)}/{currentFlow}")],
          Components = components
        };
    }

    await _kustomizationGenerator.GenerateAsync(kustomization, outputDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
