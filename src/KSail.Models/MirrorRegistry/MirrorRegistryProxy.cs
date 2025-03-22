using System.ComponentModel;

namespace KSail.Models.MirrorRegistry;


public class KSailMirrorRegistryProxy
{
  [Description("The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]")]
  public Uri Url { get; set; } = new Uri("https://registry-1.docker.io");

  // [Description("The username to authenticate with the upstream registry. [default: null]")]
  // public string? Username { get; set; }


  // [Description("The password to authenticate with the upstream registry. [default: null]")]
  // public string? Password { get; set; }


  // [Description("Connect to the upstream registry over HTTPS. [default: false]")]
  // public bool? Insecure { get; set; }
}
