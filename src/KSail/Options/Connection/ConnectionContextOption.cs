using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Connection;


class ConnectionContextOption : Option<string?>
{
  public ConnectionContextOption(KSailCluster config) : base(
    "-c", "--context"
  ) => Description = $"The kubernetes context to use. [default: {config.Spec.Connection.Context}]";
}

