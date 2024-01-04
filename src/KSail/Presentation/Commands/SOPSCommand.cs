using System.CommandLine;

namespace KSail.Presentation.Commands;

/// <summary>
/// The 'sops' command responsible for managing the KSail SOPS GPG key.
/// </summary>
public class SOPSCommand : Command
{
  /// <summary>
  /// Initializes a new instance of the <see cref="SOPSCommand"/> class.
  /// </summary>
  public SOPSCommand() : base("sops", "manage SOPS GPG key") => this.SetHandler(() => _ = this.InvokeAsync("--help"));
}
