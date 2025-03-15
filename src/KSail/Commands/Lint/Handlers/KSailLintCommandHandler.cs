using Devantler.KubernetesValidator.ClientSide.Schemas;
using Devantler.KubernetesValidator.ClientSide.YamlSyntax;
using KSail.Models;

namespace KSail.Commands.Lint.Handlers;

class KSailLintCommandHandler(KSailCluster config)
{
  readonly ConfigurationValidator _configValidator = new(config);
  readonly YamlSyntaxValidator _yamlSyntaxValidator = new();
  readonly SchemaValidator _schemaValidator = new();
  readonly KSailCluster _config = config;

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    string kubernetesDirectory = _config.Spec.Project.KustomizationPath
      .Replace("./", string.Empty, StringComparison.OrdinalIgnoreCase)
      .Split('/', StringSplitOptions.RemoveEmptyEntries).First();
    if (!Directory.Exists(kubernetesDirectory) || Directory.GetFiles(kubernetesDirectory, "*.yaml", SearchOption.AllDirectories).Length == 0)
    {
      throw new KSailException($"no manifest files found in '{kubernetesDirectory}'.");
    }

    Console.WriteLine("► validating configuration");
    var (configIsValid, configMessage) = await _configValidator.ValidateAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
    if (!configIsValid)
    {
      throw new KSailException(configMessage);
    }
    Console.WriteLine("✔ configuration is valid");

    Console.WriteLine("► validating yaml syntax");
    var (yamlIsValid, yamlMessage) = await _yamlSyntaxValidator.ValidateAsync(kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    if (!yamlIsValid)
    {
      throw new KSailException(yamlMessage);
    }
    Console.WriteLine("✔ yaml syntax is valid");

    Console.WriteLine("► validating schemas");
    var (schemasAreValid, schemasMessage) = await _schemaValidator.ValidateAsync(kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    if (!schemasAreValid)
    {
      throw new KSailException(schemasMessage);
    }
    Console.WriteLine("✔ schemas are valid");
    return yamlIsValid && schemasAreValid;
  }
}
