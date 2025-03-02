using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationReconcileOnUpOption(KSailCluster config) : Option<bool?>(
  ["--reconcile", "-r"],
  $"Reconcile manifests. [default: {config.Spec.Validation.ReconcileOnUp}]"
)
{
}
