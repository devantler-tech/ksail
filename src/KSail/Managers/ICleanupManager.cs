using KSail.Models;

namespace KSail.Managers;

interface ICleanupManager
{
  Task CleanupAsync(CancellationToken cancellationToken = default);
}
