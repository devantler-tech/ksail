using KSail.Generator;
using KSail.Models;

namespace KSail.Commands.Init.Generators.SubGenerators;

class KSailClusterConfigGenerator
{
  readonly KSailClusterGenerator _ksailClusterGenerator = new();
  internal async Task GenerateAsync(KSailCluster config, CancellationToken cancellationToken = default)
  {
    string outputPath = Path.Combine(config.Spec.Project.ConfigPath);
    if (File.Exists(outputPath))
    {
      Console.WriteLine($"✔ skipping '{outputPath}', as it already exists.");
      return;
    }
    Console.WriteLine($"✚ generating '{outputPath}'");
    await _ksailClusterGenerator.GenerateAsync(config, outputPath, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
