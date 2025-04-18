using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;



class ProjectOptions(KSailCluster config)
{
  public readonly ProjectCNIOption CNIOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectCSIOption CSIOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectConfigPathOption ConfigPathOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectDistributionConfigPathOption DistributionConfigPathOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectKustomizationPathOption KustomizationPathOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectProviderOption ProviderOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectDistributionOption DistributionOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectDeploymentToolOption DeploymentToolOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectIngressControllerOption IngressControllerOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectGatewayControllerOption GatewayControllerOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectSecretManagerOption SecretManagerOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectMirrorRegistriesOption MirrorRegistriesOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public readonly ProjectEditorOption EditorOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
}
