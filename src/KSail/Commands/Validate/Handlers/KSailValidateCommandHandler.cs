using DevantlerTech.KubernetesValidator.ClientSide.Schemas;
using DevantlerTech.KubernetesValidator.ClientSide.YamlSyntax;
using KSail.Commands.Validate.Validators;
using KSail.Models;

namespace KSail.Commands.Validate.Handlers;

class KSailValidateCommandHandler(KSailCluster config, string path) : ICommandHandler
{
  readonly ConfigurationValidator _configValidator = new(config);
  readonly YamlSyntaxValidator _yamlSyntaxValidator = new();
  readonly SchemaValidator _schemaValidator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("🔍 Validating project files and configuration...");
    if (!Directory.Exists(path) || Directory.GetFiles(path, "*.yaml", SearchOption.AllDirectories).Length == 0)
      throw new KSailException($"no manifest files found in '{path}'.");
    await _configValidator.ValidateAsync(path, cancellationToken: cancellationToken).ConfigureAwait(false);
    string kubernetesDirectory = Path.Combine(path, string.Join("/", config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First()));
    if (!Directory.Exists(kubernetesDirectory))
    {
      kubernetesDirectory = path;
    }
    Console.WriteLine("► validating yaml syntax");
    var (yamlIsValid, yamlMessage) = await _yamlSyntaxValidator.ValidateAsync(kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    if (!yamlIsValid)
      throw new KSailException(yamlMessage);
    Console.WriteLine("✔ yaml syntax is valid");

    Console.WriteLine("► validating schemas");
    var (schemasAreValid, schemasMessage) = await _schemaValidator.ValidateAsync(kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    if (!schemasAreValid)
      throw new KSailException(schemasMessage);
    Console.WriteLine("✔ schemas are valid");
  }
}
