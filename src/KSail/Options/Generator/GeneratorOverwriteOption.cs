using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Generator;

class GeneratorOverwriteOption : Option<bool?>
{
  public GeneratorOverwriteOption(KSailCluster config) : base(
    "--overwrite"
  )
  {
    Description = "Overwrite existing files.";
    DefaultValueFactory = (result) => config.Spec.Generator.Overwrite;
  }
}

