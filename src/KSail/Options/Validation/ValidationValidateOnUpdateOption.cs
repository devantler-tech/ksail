using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationValidateOnUpdateOption : Option<bool?>
{
  public ValidationValidateOnUpdateOption(KSailCluster config) : base(
    "--validate", "-v"
  )
  {
    Description = "Validate project files on update.";
    DefaultValueFactory = (result) => config.Spec.Validation.ValidateOnUpdate;
  }
}

