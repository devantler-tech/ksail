using System.CommandLine;
using System.Diagnostics.CodeAnalysis;
using System.Globalization;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;
using KSail.Models;

namespace KSail.Commands.Secrets.Handlers;

[ExcludeFromCodeCoverage]
class KSailSecretsEditCommandHandler(KSailCluster config, string path, ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly KSailCluster _config = config;
  readonly string _path = path;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task HandleAsync(CancellationToken cancellationToken)
  {
    Environment.SetEnvironmentVariable("EDITOR", _config.Spec.Project.Editor.ToString().ToLower(CultureInfo.CurrentCulture));
    await _secretManager.EditAsync(_path, cancellationToken).ConfigureAwait(false);
    Environment.SetEnvironmentVariable("EDITOR", null);
  }
}
