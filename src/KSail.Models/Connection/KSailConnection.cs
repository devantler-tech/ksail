using System.ComponentModel;

namespace KSail.Models.Connection;


public class KSailConnection
{

  [Description("The path to the kubeconfig file. [default: ~/.kube/config]")]
  public string Kubeconfig { get; set; } = $"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.kube/config";


  [Description("The kube context. [default: kind-ksail-default]")]
  public string Context { get; set; } = "kind-ksail-default";


  [Description("The timeout for operations (10s, 5m, 1h). [default: 5m]")]
  public string Timeout { get; set; } = "5m";
}
