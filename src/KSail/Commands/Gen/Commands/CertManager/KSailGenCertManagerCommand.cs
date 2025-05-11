
using System.CommandLine;

namespace KSail.Commands.Gen.Commands.CertManager;

class KSailGenCertManagerCommand : Command
{
  public KSailGenCertManagerCommand() : base("cert-manager", "Generate a CertManager resource.") => AddCommands();

  void AddCommands()
  {
    AddCommand(new KSailGenCertManagerCertificateCommand());
    AddCommand(new KSailGenCertManagerClusterIssuerCommand());
  }
}
