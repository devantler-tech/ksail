using System.ComponentModel;

namespace KSail.Models.LocalRegistry;


public class KSailLocalRegistry
{

  [Description("The name of the registry. [default: ksail-registry]")]
  public string Name { get; set; } = "ksail-registry";

  [Description("The host port of the registry (if applicable). [default: 5555]")]
  public int HostPort { get; set; } = 5555;

  // [Description("The username to authenticate with the registry. [default: null]")]
  // public string? Username { get; set; }

  // [Description("The password to authenticate with the registry. [default: null]")]
  // public string? Password { get; set; }

  [Description("The registry provider. [default: Docker]")]
  public KSailLocalRegistryProvider Provider { get; set; } = KSailLocalRegistryProvider.Docker;
}
