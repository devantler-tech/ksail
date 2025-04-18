using KSail.Models;

interface IBootstrapper
{
  Task BootstrapAsync(CancellationToken cancellationToken = default);
}
