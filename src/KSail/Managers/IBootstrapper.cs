using KSail.Models;

namespace KSail.Managers;

interface IBootstrapper
{
  Task BootstrapAsync(CancellationToken cancellationToken = default);
}
