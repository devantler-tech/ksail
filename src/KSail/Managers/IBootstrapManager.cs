using KSail.Models;

namespace KSail.Managers;

interface IBootstrapManager : IManager
{
  Task BootstrapAsync(CancellationToken cancellationToken = default);
}
