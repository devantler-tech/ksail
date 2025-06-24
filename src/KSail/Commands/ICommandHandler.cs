namespace KSail.Commands;

interface ICommandHandler
{
  Task HandleAsync(CancellationToken cancellationToken = default);
}
