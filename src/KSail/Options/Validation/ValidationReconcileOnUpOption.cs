using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationReconcileOnUpOption : Option<bool?>
{
  public ValidationReconcileOnUpOption(KSailCluster config) : base(
    "--reconcile", "-r"
  )
  {
    Description = "Reconcile manifests on up.";
    DefaultValueFactory = (result) => config.Spec.Validation.ReconcileOnUp;
  }
}

