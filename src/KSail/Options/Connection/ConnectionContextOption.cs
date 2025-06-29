using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Connection;


class ConnectionContextOption : Option<string?>
{
  public ConnectionContextOption(KSailCluster config) : base(
    "-c", "--context"
  )
  {
    Description = "The kubernetes context to use.";
    DefaultValueFactory = (result) => config.Spec.Connection.Context;
  }
}

