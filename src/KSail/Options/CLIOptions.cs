using KSail.Models;
using KSail.Options.CNI;
using KSail.Options.Connection;
using KSail.Options.DeploymentTool;
using KSail.Options.Distribution;
using KSail.Options.Generator;
using KSail.Options.IngressController;
using KSail.Options.LocalRegistry;
using KSail.Options.Metadata;
using KSail.Options.MirrorRegistries;
using KSail.Options.Project;
using KSail.Options.SecretManager;
using KSail.Options.Validation;
using KSail.Options.WaypointController;

namespace KSail.Options;


static class CLIOptions
{
  static readonly KSailCluster _config = new();

  public static readonly CNIOptions CNI = new();

  public static readonly ConnectionOptions Connection = new(_config);

  public static readonly DeploymentToolOptions DeploymentTool = new(_config);

  public static readonly DistributionOptions Distribution = new(_config);

  public static readonly GeneratorOptions Generator = new();

  public static readonly IngressControllerOptions IngressController = new();

  public static readonly LocalRegistryOptions LocalRegistry = new();

  public static readonly MetadataOptions Metadata = new(_config);

  public static readonly MirrorRegistriesOptions MirrorRegistries = new();

  public static readonly ProjectOptions Project = new(_config);

  public static readonly SecretManagerOptions SecretManager = new(_config);

  public static readonly ValidationOptions Validation = new(_config);

  public static readonly WaypointControllerOptions WaypointController = new();
}
