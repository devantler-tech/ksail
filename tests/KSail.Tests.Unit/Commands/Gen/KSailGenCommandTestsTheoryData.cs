using System.Diagnostics.CodeAnalysis;

namespace KSail.Tests.Unit.Commands.Gen;

static class KSailGenCommandTestsTheoryData
{
  public static TheoryData<string[]> HelpTheoryData =>
    [
      ["gen", "--helpz"],
      ["gen", "cert-manager", "--helpz"],
      ["gen", "cert-manager", "certificate", "--helpz"],
      ["gen", "cert-manager", "cluster-issuer", "--helpz"],
      ["gen", "config", "--helpz"],
      ["gen", "config", "k3d", "--helpz"],
      ["gen", "config", "ksail", "--helpz"],
      ["gen", "config", "sops", "--helpz"],
      ["gen", "flux", "--helpz"],
      ["gen", "flux", "helm-release", "--helpz"],
      ["gen", "flux", "helm-repository", "--helpz"],
      ["gen", "flux", "kustomization", "--helpz"],
      ["gen", "kustomize", "--helpz"],
      ["gen", "kustomize", "component", "--helpz"],
      ["gen", "kustomize", "kustomization", "--helpz"],
      ["gen", "native", "--helpz"],
      ["gen", "native", "cluster-role-binding", "--helpz"],
      ["gen", "native", "namespace", "--helpz"],
      ["gen", "native", "network-policy", "--helpz"],
      ["gen", "native", "persistent-volume", "--helpz"],
      ["gen", "native", "resource-quota", "--helpz"],
      ["gen", "native", "role-binding", "--helpz"],
      ["gen", "native", "role", "--helpz"],
      ["gen", "native", "service-account", "--helpz"],
      ["gen", "native", "config-map", "--helpz"],
      ["gen", "native", "persistent-volume-claim", "--helpz"],
      ["gen", "native", "secret", "--helpz"],
      ["gen", "native", "horizontal-pod-autoscaler", "--helpz"],
      ["gen", "native", "pod-disruption-budget", "--helpz"],
      ["gen", "native", "ingress", "--helpz"],
      ["gen", "native", "service", "--helpz"],
      ["gen", "native", "cron-job", "--helpz"],
      ["gen", "native", "daemon-set", "--helpz"],
      ["gen", "native", "deployment", "--helpz"],
      ["gen", "native", "job", "--helpz"],
      ["gen", "native", "stateful-set", "--helpz"]
    ];
  public static TheoryData<string[], string> GenerateNativeResourceTheoryData =>
    new()
    {
      { ["gen", "native", "cluster-role"], "cluster-role.yaml" },
      { ["gen", "native", "cluster-role-binding"], "cluster-role-binding.yaml" },
      { ["gen", "native", "namespace"], "namespace.yaml" },
      { ["gen", "native", "network-policy"], "network-policy.yaml" },
      { ["gen", "native", "persistent-volume"], "persistent-volume.yaml" },
      { ["gen", "native", "resource-quota"], "resource-quota.yaml" },
      { ["gen", "native", "role-binding"], "role-binding.yaml" },
      { ["gen", "native", "role"], "role.yaml" },
      { ["gen", "native", "service-account"], "service-account.yaml" },
      { ["gen", "native", "config-map"], "config-map.yaml" },
      { ["gen", "native", "persistent-volume-claim"], "persistent-volume-claim.yaml" },
      { ["gen", "native", "secret"], "secret.yaml" },
      { ["gen", "native", "horizontal-pod-autoscaler"], "horizontal-pod-autoscaler.yaml" },
      { ["gen", "native", "pod-disruption-budget"], "pod-disruption-budget.yaml" },
      { ["gen", "native", "priority-class"], "priority-class.yaml" },
      { ["gen", "native", "ingress"], "ingress.yaml" },
      { ["gen", "native", "service"], "service.yaml" },
      { ["gen", "native", "cron-job"], "cron-job.yaml" },
      { ["gen", "native", "daemon-set"], "daemon-set.yaml" },
      { ["gen", "native", "deployment"], "deployment.yaml" },
      { ["gen", "native", "job"], "job.yaml" },
      { ["gen", "native", "stateful-set"], "stateful-set.yaml" }
    };
  public static TheoryData<string[], string> GenerateCertManagerResourceTheoryData =>
    new()
    {
      { ["gen", "cert-manager", "certificate"], "cert-manager-certificate.yaml" },
      { ["gen", "cert-manager", "cluster-issuer"], "cert-manager-cluster-issuer.yaml" }
    };
  public static TheoryData<string[], string> GenerateConfigResourceTheoryData =>
    new()
    {
      { ["gen", "config", "k3d"], "k3d.yaml" },
      { ["gen", "config", "ksail"], "ksail.yaml" },
      { ["gen", "config", "sops"], ".sops.yaml" }
    };
  public static TheoryData<string[], string> GenerateFluxResourceTheoryData =>
    new()
    {
      { ["gen", "flux", "helm-release"], "flux-helm-release.yaml" },
      { ["gen", "flux", "helm-repository"], "flux-helm-repository.yaml" },
      { ["gen", "flux", "kustomization"], "flux-kustomization.yaml" }
    };
  public static TheoryData<string[], string> GenerateKustomizeResourceTheoryData =>
    new()
    {
      { ["gen", "kustomize", "component"], "kustomize-component.yaml" },
      { ["gen", "kustomize", "kustomization"], "kustomize-kustomization.yaml" }
    };
}
