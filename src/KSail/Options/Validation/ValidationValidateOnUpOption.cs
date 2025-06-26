using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationValidateOnUpOption : Option<bool?>
{
  public ValidationValidateOnUpOption(KSailCluster config) : base(
    "--validate", "-v"
  )
  {
    Description = "Validate project files on up.";
    DefaultValueFactory = (result) => config.Spec.Validation.ValidateOnUp;
  }
}

