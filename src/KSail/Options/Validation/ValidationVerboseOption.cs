using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;

class ValidationVerboseOption(KSailCluster config) : Option<bool?>(
  ["--verbose"],
  $"Verbose output for validation or status checks. [default: {config.Spec.Validation.Verbose}]"
);
