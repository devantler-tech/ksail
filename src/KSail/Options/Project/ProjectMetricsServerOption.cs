using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectMetricsServerOption(KSailCluster config) : Option<bool?>(
  ["-ms", "--metrics-server"],
  $"Whether to install Metrics Server. [default: {config.Spec.Project.MetricsServer}]"
);
