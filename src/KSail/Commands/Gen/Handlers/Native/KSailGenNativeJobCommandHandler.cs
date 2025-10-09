using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using DevantlerTech.KubernetesGenerator.Native.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeWorkloadsJobCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly JobGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new Job
    {
      Metadata = new Metadata
      {
        Name = "my-job",
      },
      Spec = new JobSpec
      {
        Image = "my-image",
      },
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
