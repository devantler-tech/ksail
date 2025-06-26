using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Connection;


class ConnectionTimeoutOption : Option<string?>
{
  public ConnectionTimeoutOption(KSailCluster config) : base(
    "-t", "--timeout"
  )
  {
    Description = "The time to wait for each kustomization to become ready.";
    DefaultValueFactory = (result) => config.Spec.Connection.Timeout;
  }
}

