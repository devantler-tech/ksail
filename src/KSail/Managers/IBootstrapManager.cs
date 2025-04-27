using KSail.Models;

namespace KSail.Managers;

interface IBootstrapManager
{
  Task BootstrapAsync(CancellationToken cancellationToken = default);
}
