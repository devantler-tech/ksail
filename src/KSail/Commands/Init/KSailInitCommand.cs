using System.CommandLine;
using KSail.Arguments;
using KSail.Commands.Init.Handlers;
using KSail.Options;

namespace KSail.Commands.Init;

sealed class KSailInitCommand : Command
{
  readonly ClusterNameArgument _clusterNameArgument = new();
  readonly ManifestsOption _manifestsOption = new() { IsRequired = true };
  public KSailInitCommand() : base("init", "Initialize a new K8s cluster")
  {
    AddArgument(_clusterNameArgument);
    AddOption(_manifestsOption);

    this.SetHandler(KSailInitCommandHandler.HandleAsync, _clusterNameArgument, _manifestsOption);
  }
}
