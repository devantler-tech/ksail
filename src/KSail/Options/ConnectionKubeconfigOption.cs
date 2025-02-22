using System.CommandLine;
using KSail.Models;

namespace KSail.Options;

/// <summary>
/// The kubeconfig to use for the connection.
/// </summary>
public class ConnectionKubeconfigOption(KSailCluster config) : Option<string>(
  ["-k", "--kubeconfig"],
  $"Path to kubeconfig file. Default: '{config.Spec.Connection.Kubeconfig}' (G)"
)
{
}
