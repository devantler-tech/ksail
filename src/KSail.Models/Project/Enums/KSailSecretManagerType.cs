using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailSecretManagerType
{
  [Description("Do not use a secret manager.")]
  None,
  [Description("Use SOPS to manage sensitive data in the project.")]
  SOPS
}
