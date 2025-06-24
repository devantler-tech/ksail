using DevantlerTech.SecretManager.SOPS.LocalAge.Models;
using DevantlerTech.SecretManager.SOPS.LocalAge.Utils;

namespace KSail.Utils;

static class SopsConfigLoader
{
  static readonly SOPSConfigHelper _sopsConfigHelper = new();
  internal static async Task<SOPSConfig> LoadAsync(string? searchPath = default, CancellationToken cancellationToken = default)
  {
    searchPath ??= Directory.GetCurrentDirectory();
    Console.WriteLine($"► searching for a '.sops.yaml' file in '{searchPath}' and its parent directories");
    string sopsConfigPath = string.Empty;
    while (!string.IsNullOrEmpty(searchPath))
    {
      if (File.Exists(Path.Combine(searchPath, ".sops.yaml")))
      {
        sopsConfigPath = Path.Combine(searchPath, ".sops.yaml");
        Console.WriteLine($"✔ found '{sopsConfigPath}'");
        break;
      }
      searchPath = Directory.GetParent(searchPath)?.FullName ?? string.Empty;
    }
    if (string.IsNullOrEmpty(sopsConfigPath))
    {
      throw new KSailException("'.sops.yaml' file not found in the current or parent directories");
    }
    Console.WriteLine("► reading public key from '.sops.yaml' file");
    var sopsConfig = await _sopsConfigHelper.GetSOPSConfigAsync(sopsConfigPath, cancellationToken).ConfigureAwait(false);
    return sopsConfig;
  }
}


