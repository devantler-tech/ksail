using System.CommandLine;
using KSail.Models;

namespace KSail.Options;

/// <summary>
/// The kube context to use for the connection.
/// </summary>
public class ConnectionContextOption(KSailCluster config) : Option<string>(
  ["-c", "--context"],
  $"The kubernetes context to use. Default: '{config.Spec.Connection.Context}' (G)"
)
{
}
