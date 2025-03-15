using System.ComponentModel;

namespace KSail.Models.Validation;


public class KSailValidation
{

  [Description("Lint the manifests before applying them to a new cluster. [default: true]")]
  public bool LintOnUp { get; set; } = true;


  [Description("Wait for reconciliation to succeed on a new cluster. [default: true]")]
  public bool ReconcileOnUp { get; set; } = true;


  [Description("Lint the manifests before applying them to an existing cluster. [default: true]")]
  public bool LintOnUpdate { get; set; } = true;


  [Description("Wait for reconciliation to succeed on an existing cluster. [default: true]")]
  public bool ReconcileOnUpdate { get; set; } = true;
}
