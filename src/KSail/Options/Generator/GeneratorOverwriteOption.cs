using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Generator;

class GeneratorOverwriteOption(KSailCluster config) : Option<bool?>(
  ["--overwrite"],
  $"Overwrite existing files. [default: {config.Spec.Generator.Overwrite}]"
);
