using System.CommandLine;
using Devantler.K9sCLI;
using KSail.Models;

namespace KSail.Options;

/// <summary>
/// The editor to use for the project.
/// </summary>
public class ProjectEditorOption(KSailCluster config) : Option<Editor?>(
  ["--editor", "-e"],
  $"Editor to use. Default: '{config.Spec.Project.Editor}' (G)"
);
