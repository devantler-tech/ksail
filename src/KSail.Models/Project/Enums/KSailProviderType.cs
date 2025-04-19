using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailProviderType
{
  [Description("Use Docker as the engine.")]
  Docker,

  [Description("Use Podman as the engine.")]
  Podman

  //
  // ClusterAPI
}
