using System.Runtime.InteropServices;

class PodmanHelper
{
  internal static string GetPodmanSocket()
  {
    return File.Exists($"/run/user/{Environment.GetEnvironmentVariable("EUID")}/podman/podman.sock") ?
      $"unix:///run/user/${Environment.GetEnvironmentVariable("EUID")}/podman/podman.sock" : File.Exists($"/run/user/{Environment.GetEnvironmentVariable("UID")}/podman/podman.sock") ?
      $"unix:///run/user/${Environment.GetEnvironmentVariable("UID")}/podman/podman.sock" : File.Exists("/run/podman/podman.sock") ?
      "unix:///run/podman/podman.sock" : "unix:///var/run/docker.sock";
  }
}
