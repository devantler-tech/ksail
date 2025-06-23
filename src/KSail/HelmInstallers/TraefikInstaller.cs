
using DevantlerTech.HelmCLI;
using DevantlerTech.KubectlCLI;
using KSail.Models;

namespace KSail.HelmInstallers;

class TraefikInstaller(KSailCluster config) : IHelmInstaller
{
  readonly KSailCluster _config = config;
  public async Task AddRepositoryAsync(CancellationToken cancellationToken = default)
  {
    string[] helmAddRepoArguments = [
      "repo",
      "add",
      "traefik",
      "https://traefik.github.io/charts"
    ];
    _ = await Helm.RunAsync(helmAddRepoArguments, cancellationToken: cancellationToken).ConfigureAwait(false);
  }

  public async Task InstallAsync(CancellationToken cancellationToken = default)
  {
    string[] helmInstallArguments = [
      "install",
      "traefik",
      "traefik/traefik",
      "--namespace", "traefik",
      "--create-namespace",
      "--wait",
      "--kubeconfig", _config.Spec.Connection.Kubeconfig,
      "--kube-context", _config.Spec.Connection.Context
    ];
    _ = await Helm.RunAsync(helmInstallArguments, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
