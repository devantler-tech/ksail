using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationReconcileOnUpdateOption : Option<bool?>
{
  public ValidationReconcileOnUpdateOption(KSailCluster config) : base(
    "--reconcile", "-r"
  )
  {
    Description = "Reconcile manifests on update.";
    DefaultValueFactory = (result) => config.Spec.Validation.ReconcileOnUpdate;
  }
}

