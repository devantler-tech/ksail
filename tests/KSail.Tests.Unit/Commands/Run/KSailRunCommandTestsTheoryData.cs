using System.Diagnostics.CodeAnalysis;

namespace KSail.Tests.Unit.Commands.Run;

static class KSailRunCommandTestsTheoryData
{
  public static TheoryData<string[]> HelpTheoryData =>
    [
      ["run", "--helpz"]
    ];

  public static TheoryData<string[]> RunTheoryData =>
    [
      ["run", "age-keygen", "--help"],
      ["run", "cilium", "--help"],
      ["run", "flux", "--help"],
      ["run", "helm", "--help"],
      ["run", "k3d", "--help"],
      ["run", "k9s", "--help"],
      ["run", "kind", "--help"],
      ["run", "kubeconform", "-h"],
      ["run", "kubectl", "--help"],
      ["run", "kustomize", "--help"],
      ["run", "sops", "--help"]
    ];
}
