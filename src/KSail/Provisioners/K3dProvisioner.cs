using KSail.CLIWrappers;

namespace KSail.Provisioners;

sealed class K3dProvisioner() : IProvisioner
{
  internal static async Task ProvisionAsync(string name, bool pullThroughRegistries, string? configPath = null)
  {
    console.WriteLine($"🚀 Provisioning K3d cluster '{name}'...");
    if (!string.IsNullOrEmpty(configPath))
    {
      await K3dCLIWrapper.CreateClusterFromConfigAsync(configPath);
    }
    else
    {
      await K3dCLIWrapper.CreateClusterAsync(name, pullThroughRegistries);
    }
    console.WriteLine();
  }

  internal static async Task DeprovisionAsync(string name)
  {
    console.WriteLine($"🔥 Destroying K3d cluster '{name}'...");
    await K3dCLIWrapper.DeleteClusterAsync(name);
  }

  internal static async Task ListAsync()
  {
    console.WriteLine("📋 Listing K3d clusters...");
    await K3dCLIWrapper.ListClustersAsync();
  }

  internal static async Task<bool> ExistsAsync(string name) =>
    await K3dCLIWrapper.GetClusterAsync(name);
}
