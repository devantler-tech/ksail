using System.ComponentModel;

namespace KSail.Models;


public class KSailMetadata
{
  [Description("The name of the KSail object. [default: ksail-default]")]
  public required string Name { get; set; } = "ksail-default";
}
