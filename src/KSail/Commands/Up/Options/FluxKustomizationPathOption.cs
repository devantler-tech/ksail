using System.CommandLine;

namespace KSail.Commands.Up.Options;

sealed class FluxKustomizationPathOption() : Option<string>(
 ["--flux-kustomization-path", "-fkp"],
  "Path to the flux kustomization directory"
);
