namespace KSail.Commands;

interface ICommandHandler
{
  Task<int> HandleAsync(CancellationToken cancellationToken = default);
}
