using System.ComponentModel;

namespace KSail.Models.Validation;


public class KSailValidation
{

  [Description("Validate the project files and configuration before creating a new cluster. [default: true]")]
  public bool ValidateOnUp { get; set; } = true;


  [Description("Wait for reconciliation to succeed on a new cluster. [default: true]")]
  public bool ReconcileOnUp { get; set; } = true;


  [Description("Validate the project files and configuration before applying changes to an existing cluster. [default: true]")]
  public bool ValidateOnUpdate { get; set; } = true;


  [Description("Wait for reconciliation to succeed on an existing cluster. [default: true]")]
  public bool ReconcileOnUpdate { get; set; } = true;
}
