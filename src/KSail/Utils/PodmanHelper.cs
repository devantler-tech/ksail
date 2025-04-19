using System.Runtime.InteropServices;

class PodmanHelper
{
  internal static string GetPodmanSocket()
  {
    return RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ?
      "npipe://./pipe/docker_engine" : File.Exists($"/run/user/{Environment.GetEnvironmentVariable("EUID")}/podman/podman.sock") ?
      $"unix:/run/user/${Environment.GetEnvironmentVariable("EUID")}/podman/podman.sock" : File.Exists($"/run/user/{Environment.GetEnvironmentVariable("UID")}/podman/podman.sock") ?
      $"unix:/run/user/${Environment.GetEnvironmentVariable("UID")}/podman/podman.sock" : "unix:/var/run/docker.sock";
  }
}
