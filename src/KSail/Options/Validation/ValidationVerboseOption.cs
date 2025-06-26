using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;

class ValidationVerboseOption : Option<bool?>
{
  public ValidationVerboseOption(KSailCluster config) : base(
    "--verbose"
  )
  {
    Description = "Verbose output for validation or status checks.";
    DefaultValueFactory = (result) => config.Spec.Validation.Verbose;
  }
}

