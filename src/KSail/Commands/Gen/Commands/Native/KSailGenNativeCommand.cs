
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.Native;

class KSailGenNativeCommand : Command
{

  public KSailGenNativeCommand() : base("native", "Generate a native Kubernetes resource.") => AddCommands();

  void AddCommands()
  {
    AddCommand(new KSailGenNativeClusterRoleBindingCommand());
    AddCommand(new KSailGenNativeClusterRoleCommand());
    AddCommand(new KSailGenNativeNamespaceCommand());
    AddCommand(new KSailGenNativeNetworkPolicyCommand());
    AddCommand(new KSailGenNativePersistentVolumeCommand());
    AddCommand(new KSailGenNativeResourceQuotaCommand());
    AddCommand(new KSailGenNativeRoleBindingCommand());
    AddCommand(new KSailGenNativeRoleCommand());
    AddCommand(new KSailGenNativeAccountCommand());

    AddCommand(new KSailGenNativeConfigMapCommand());
    AddCommand(new KSailGenNativePersistentVolumeClaimCommand());
    AddCommand(new KSailGenNativeSecretCommand());

    AddCommand(new KSailGenNativeHorizontalPodAutoscalerCommand());
    AddCommand(new KSailGenNativePodDisruptionBudgetCommand());
    AddCommand(new KSailGenNativePriorityClassCommand());

    AddCommand(new KSailGenNativeIngressCommand());
    AddCommand(new KSailGenNativeServiceCommand());

    AddCommand(new KSailGenNativeWorkloadsCronJobCommand());
    AddCommand(new KSailGenNativeWorkloadsDaemonSetCommand());
    AddCommand(new KSailGenNativeWorkloadsDeploymentCommand());
    AddCommand(new KSailGenNativeWorkloadsJobCommand());
    AddCommand(new KSailGenNativeWorkloadsStatefulSetCommand());
  }
}
