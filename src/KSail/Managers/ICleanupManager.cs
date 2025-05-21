using KSail.Models;

namespace KSail.Managers;

interface ICleanupManager : IManager
{
  Task CleanupAsync(CancellationToken cancellationToken = default);
}
