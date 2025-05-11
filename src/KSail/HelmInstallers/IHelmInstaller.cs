namespace KSail.HelmInstallers;

interface IHelmInstaller
{
  Task AddRepositoryAsync(CancellationToken cancellationToken = default);
  Task InstallAsync(CancellationToken cancellationToken = default);
}
