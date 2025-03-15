using System.ComponentModel;

namespace KSail.Models.Generator;

public class KSailGenerator
{
  [Description("Overwrite existing files. [default: false]")]
  public bool Overwrite { get; set; }
}
