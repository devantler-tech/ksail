using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Connection;


class ConnectionOptions(KSailCluster config)
{

  public readonly ConnectionContextOption ContextOption = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public readonly ConnectionKubeconfigOption KubeconfigOption = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public readonly ConnectionTimeoutOption TimeoutOption = new(config) { Arity = ArgumentArity.ZeroOrOne };
}
