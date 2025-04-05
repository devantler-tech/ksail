using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationValidateOnUpdateOption(KSailCluster config) : Option<bool?>(
  ["--validate", "-v"],
  $"Validate project files and configuration before applying changes to an existing cluster. [default: {config.Spec.Validation.ValidateOnUpdate}]"
)
{
}
