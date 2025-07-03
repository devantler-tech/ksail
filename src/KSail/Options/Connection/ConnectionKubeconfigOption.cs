using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Connection;


class ConnectionKubeconfigOption : Option<string?>
{
  public ConnectionKubeconfigOption(KSailCluster config) : base(
    "-k", "--kubeconfig"
  )
  {
    Description = "Path to kubeconfig file.";
    DefaultValueFactory = (result) => config.Spec.Connection.Kubeconfig;
  }
}
