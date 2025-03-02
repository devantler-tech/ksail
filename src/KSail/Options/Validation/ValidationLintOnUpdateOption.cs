using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationLintOnUpdateOption(KSailCluster config) : Option<bool?>(
  ["--lint", "-l"],
  $"Lit manifests. [default: {config.Spec.Validation.LintOnUpdate}]"
)
{
}
