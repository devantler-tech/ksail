using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;



class ProjectOptions(KSailCluster config)
{
  public readonly ProjectConfigPathOption ConfigPathOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectDeploymentToolOption DeploymentToolOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectDistributionConfigPathOption DistributionConfigPathOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectDistributionOption DistributionOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectEditorOption EditorOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectEngineOption EngineOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectKustomizationPathOption KustomizationPathOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectMirrorRegistriesOption MirrorRegistriesOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectSecretManagerOption SecretManagerOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
}
