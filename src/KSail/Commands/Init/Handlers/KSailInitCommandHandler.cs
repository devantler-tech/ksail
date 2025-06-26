using KSail.Commands.Init.Generators;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Init.Handlers;

class KSailInitCommandHandler(string outputPath, KSailCluster config) : ICommandHandler
{
  readonly KSailCluster _config = config;
  readonly string _outputPath = outputPath;
  readonly SOPSConfigFileGenerator _sopsConfigFileGenerator = new();
  readonly KSailClusterConfigGenerator _ksailClusterConfigGenerator = new();
  readonly DistributionConfigFileGenerator _distributionConfigFileGenerator = new();
  readonly ProjectGenerator _projectGenerator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"📁 Initializing project...");
    await _ksailClusterConfigGenerator.GenerateAsync(
      _outputPath,
      _config,
      cancellationToken
    ).ConfigureAwait(false);

    await _distributionConfigFileGenerator.GenerateAsync(
      _outputPath,
      _config,
      cancellationToken
    ).ConfigureAwait(false);

    switch (_config.Spec.Project.SecretManager)
    {
      case KSailSecretManagerType.SOPS:
        await _sopsConfigFileGenerator.GenerateAsync(
            _outputPath,
            _config,
            cancellationToken
        ).ConfigureAwait(false);
        break;
      case KSailSecretManagerType.None:
        break;
      default:
        throw new NotSupportedException($"Secret manager '{_config.Spec.Project.SecretManager}' is not supported.");
    }

    await _projectGenerator.GenerateAsync(
      _outputPath,
      _config,
      cancellationToken

    ).ConfigureAwait(false);

  }
}
