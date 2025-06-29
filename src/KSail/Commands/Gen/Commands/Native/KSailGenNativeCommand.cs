
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Native;

class KSailGenNativeCommand : Command
{

  public KSailGenNativeCommand() : base("native", "Generate a native Kubernetes resource.") => AddCommands();

  void AddCommands()
  {
    Subcommands.Add(new KSailGenNativeClusterRoleBindingCommand());
    Subcommands.Add(new KSailGenNativeClusterRoleCommand());
    Subcommands.Add(new KSailGenNativeNamespaceCommand());
    Subcommands.Add(new KSailGenNativeNetworkPolicyCommand());
    Subcommands.Add(new KSailGenNativePersistentVolumeCommand());
    Subcommands.Add(new KSailGenNativeResourceQuotaCommand());
    Subcommands.Add(new KSailGenNativeRoleBindingCommand());
    Subcommands.Add(new KSailGenNativeRoleCommand());
    Subcommands.Add(new KSailGenNativeAccountCommand());

    Subcommands.Add(new KSailGenNativeConfigMapCommand());
    Subcommands.Add(new KSailGenNativePersistentVolumeClaimCommand());
    Subcommands.Add(new KSailGenNativeSecretCommand());

    Subcommands.Add(new KSailGenNativeHorizontalPodAutoscalerCommand());
    Subcommands.Add(new KSailGenNativePodDisruptionBudgetCommand());
    Subcommands.Add(new KSailGenNativePriorityClassCommand());

    Subcommands.Add(new KSailGenNativeIngressCommand());
    Subcommands.Add(new KSailGenNativeServiceCommand());

    Subcommands.Add(new KSailGenNativeWorkloadsCronJobCommand());
    Subcommands.Add(new KSailGenNativeWorkloadsDaemonSetCommand());
    Subcommands.Add(new KSailGenNativeWorkloadsDeploymentCommand());
    Subcommands.Add(new KSailGenNativeWorkloadsJobCommand());
    Subcommands.Add(new KSailGenNativeWorkloadsStatefulSetCommand());
  }
}
