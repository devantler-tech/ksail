using KSail.Commands.Init.Generators;
using KSail.Commands.Init.Generators.SubGenerators;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Init.Handlers;

class KSailInitCommandHandler(KSailCluster config)
{
  readonly KSailCluster _config = config;
  readonly SOPSConfigFileGenerator _sopsConfigFileGenerator = new();
  readonly KSailClusterConfigGenerator _ksailClusterConfigGenerator = new();
  readonly DistributionConfigFileGenerator _distributionConfigFileGenerator = new();
  readonly ProjectGenerator _projectGenerator = new();

  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    await _ksailClusterConfigGenerator.GenerateAsync(
      _config,
      cancellationToken
    ).ConfigureAwait(false);

    await _distributionConfigFileGenerator.GenerateAsync(
      _config,
      cancellationToken
    ).ConfigureAwait(false);

    if (_config.Spec.Project.SecretManager)
    {
      await _sopsConfigFileGenerator.GenerateAsync(
        _config,
        cancellationToken
      ).ConfigureAwait(false);
    }

    await _projectGenerator.GenerateAsync(_config, cancellationToken).ConfigureAwait(false);

    return 0;
  }
}
