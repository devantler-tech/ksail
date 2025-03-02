using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationLintOnUpOption(KSailCluster config) : Option<bool?>(
  ["--lint", "-l"],
  $"Lit manifests. [default: {config.Spec.Validation.LintOnUp}'"
)
{
}
