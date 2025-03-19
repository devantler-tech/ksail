using System.ComponentModel;

namespace KSail.Models.Project.Enums;

public enum KSailMirrorRegistriesType
{
  [Description("Do not set up mirror registries for the project.")]
  None,
  [Description("Set up mirror registries for the project using 'registry:2'")]
  DockerRegistry
}
