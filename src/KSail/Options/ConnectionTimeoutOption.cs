using System.CommandLine;
using KSail.Models;

namespace KSail.Options;

/// <summary>
/// The timeout for the connection.
/// </summary>
public class ConnectionTimeoutOption(KSailCluster config) : Option<string>(
  ["-t", "--timeout"],
  $"The time to wait for each kustomization to become ready. Default: '{config.Spec.Connection.Timeout}' (G)"
)
{
}
