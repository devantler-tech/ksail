using System.Collections;
using System.CommandLine;

namespace KSail.Options;

/// <summary>
/// Global options for the KSail CLI.
/// </summary>
public class GlobalOptions : IReadOnlyList<Option>
{
  /// <inheritdoc />
  public Option this[int index] => Options[index];

  /// <summary>
  /// List of global options for the KSail CLI.
  /// </summary>
  public List<Option> Options { get; } =
  [
    new MetadataNameOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ProjectDistributionOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ProjectEngineOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ProjectMirrorRegistriesOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ProjectSecretManagerOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new PathOption($"Path to the distribution configuration file. Default: 'kind-config.yaml' (G)", ["--distribution-config", "-dc"]) { Arity = ArgumentArity.ZeroOrOne },
    new PathOption("The root directory of the project. Default: '.' (G)", ["--working-directory", "-wd"]) { Arity = ArgumentArity.ZeroOrOne },
    new PathOption("The path to the ksail configuration file. Default: 'ksail-config.yaml' (G)", ["--config", "-c"]) { Arity = ArgumentArity.ZeroOrOne },
    new ProjectTemplateOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ProjectEditorOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ConnectionKubeconfigOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ConnectionContextOption(new()) { Arity = ArgumentArity.ZeroOrOne },
    new ConnectionTimeoutOption(new()) { Arity = ArgumentArity.ZeroOrOne }
  ];


  /// <inheritdoc />
  public int Count => Options.Count;

  /// <inheritdoc />
  public IEnumerator<Option> GetEnumerator() => Options.GetEnumerator();
  IEnumerator IEnumerable.GetEnumerator() => GetEnumerator();
}
