
using DevantlerTech.HelmCLI;
using DevantlerTech.KubectlCLI;
using KSail.Models;

namespace KSail.HelmInstallers;

class MetricsServerInstaller(KSailCluster config) : IHelmInstaller
{
  readonly KSailCluster _config = config;
  public async Task AddRepositoryAsync(CancellationToken cancellationToken = default)
  {
    string[] helmAddRepoArguments = [
      "repo",
      "add",
      "metrics-server",
      "https://kubernetes-sigs.github.io/metrics-server/"
    ];
    _ = await Helm.RunAsync(helmAddRepoArguments, cancellationToken: cancellationToken).ConfigureAwait(false);
  }

  public async Task InstallAsync(CancellationToken cancellationToken = default)
  {
    string[] helmInstallArguments = [
      "install",
      "metrics-server",
      "metrics-server/metrics-server",
      "--namespace", "kube-system",
      "--set", "args[0]=--kubelet-insecure-tls",
      "--wait",
      "--kubeconfig", _config.Spec.Connection.Kubeconfig,
      "--kube-context", _config.Spec.Connection.Context
    ];
    _ = await Helm.RunAsync(helmInstallArguments, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
