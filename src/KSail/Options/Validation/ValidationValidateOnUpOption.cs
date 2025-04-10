using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationValidateOnUpOption(KSailCluster config) : Option<bool?>(
  ["--validate", "-v"],
  $"Validate project files before creating a new cluster. [default: {config.Spec.Validation.ValidateOnUp}]"
);
