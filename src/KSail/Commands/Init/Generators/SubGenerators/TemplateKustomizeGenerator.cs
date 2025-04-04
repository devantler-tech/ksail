using Devantler.KubernetesGenerator.Kustomize;
using Devantler.KubernetesGenerator.Kustomize.Models;
using KSail.Models;

namespace KSail.Commands.Init.Generators.SubGenerators;

class TemplateKustomizeGenerator
{
  readonly KustomizeKustomizationGenerator _kustomizeKustomizationGenerator = new();
  internal async Task GenerateAsync(KSailCluster config, CancellationToken cancellationToken = default)
  {
    string outputDirectory = Path.IsPathRooted(config.Spec.Project.KustomizationPath)
      ? config.Spec.Project.KustomizationPath
      : Path.Combine(config.Spec.Project.KustomizationPath);

    if (!Directory.Exists(outputDirectory))
      _ = Directory.CreateDirectory(outputDirectory);
    await GenerateKustomization(config, outputDirectory, cancellationToken).ConfigureAwait(false);
  }

  async Task GenerateKustomization(KSailCluster config, string outputPath, CancellationToken cancellationToken = default)
  {
    var attr = File.GetAttributes(outputPath);
    if (!attr.HasFlag(FileAttributes.Directory))
    {
      throw new KSailException($"'{nameof(config.Spec.Project.KustomizationPath)}' must be a directory. It was '{outputPath}'.");
    }
    outputPath = Path.Combine(outputPath, "kustomization.yaml");
    bool overwrite = config.Spec.Generator.Overwrite;
    Console.WriteLine(File.Exists(outputPath) ? (overwrite ?
      $"✚ overwriting '{outputPath}'" :
      $"✔ skipping '{outputPath}', as it already exists.") :
      $"✚ generating '{outputPath}'");
    if (File.Exists(outputPath) && !overwrite)
    {
      return;
    }
    var kustomization = new KustomizeKustomization()
    {
      Resources = []
    };
    await _kustomizeKustomizationGenerator.GenerateAsync(kustomization, outputPath, config.Spec.Generator.Overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
