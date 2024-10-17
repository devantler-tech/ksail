using Devantler.KeyManager.Local.Age;

namespace KSail.Commands.SOPS.Handlers;

class KSailSOPSCommandHandler() : IDisposable
{
  readonly LocalAgeKeyManager _keyManager = new();
  internal async Task<int> HandleAsync(string clusterName, bool generateKey, bool showKey, bool showPublicKey, bool showPrivateKey, string encrypt, string decrypt, string import, string export, CancellationToken cancellationToken)
  {
    switch (generateKey, showKey, showPublicKey, showPrivateKey, encrypt, decrypt, import, export)
    {
      // case (true, false, false, false, "", "", "", ""):
      //   return await HandleGenerateKey(clusterName, cancellationToken);
      // case (false, true, false, false, "", "", "", ""):
      //   return await HandleShowKey(clusterName, cancellationToken);
      case (false, false, true, false, "", "", "", ""):
        return await HandleShowPublicKey(clusterName, cancellationToken).ConfigureAwait(false);
      case (false, false, false, true, "", "", "", ""):
        return await HandleShowPrivateKey(clusterName, cancellationToken).ConfigureAwait(false);
      case (false, false, false, false, not null, "", "", ""):
        return await HandleEncrypt(encrypt, clusterName, cancellationToken).ConfigureAwait(false);
      case (false, false, false, false, "", not null, "", ""):
        return await HandleDecrypt(decrypt, clusterName, cancellationToken).ConfigureAwait(false);
      case (false, false, false, false, "", "", not null, ""):
        return await HandleImport(clusterName, import, cancellationToken).ConfigureAwait(false);
      case (false, false, false, false, "", "", "", not null):
        return await HandleExport(clusterName, export, cancellationToken).ConfigureAwait(false);
      default:
        Console.WriteLine("✕ More than one option specified");
        return 1;
    }
  }

  async Task HandleGenerateKey(string clusterName, CancellationToken cancellationToken)
  {
    string sopsYaml = FileSearcher.FindClosestAncestor(".sops.yaml", Directory.GetCurrentDirectory());

    Console.WriteLine($"🔐 Generating SOPS key for '{clusterName}'");
    var ageKey = await _keyManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false);

    // Save the key to the closest ancestor '.sops.yaml'
  }

  //static async Task<int> HandleShowKey(string clusterName, CancellationToken cancellationToken) => throw new NotImplementedException();

  async Task<int> HandleShowPrivateKey(string clusterName, CancellationToken cancellationToken)
  {
    var (exitCode, privateKey) = await _keyManager.GetPrivateKeyAsync(KeyType.Age, clusterName, cancellationToken).ConfigureAwait(false);
    if (exitCode != 0)
    {
      Console.WriteLine("✕ Private SOPS key not found");
      return 1;
    }
    Console.WriteLine(privateKey);
    return 0;
  }

  async Task<int> HandleShowPublicKey(string clusterName, CancellationToken cancellationToken)
  {
    var (exitCode, publicKey) = await _LocalProvisioner.GetPublicKeyAsync(KeyType.Age, clusterName, cancellationToken).ConfigureAwait(false);
    if (exitCode != 0)
    {
      Console.WriteLine("✕ Public SOPS key not found");
      return 1;
    }
    Console.WriteLine(publicKey);
    return 0;
  }

  static async Task<int> HandleDecrypt(string decrypt, string clusterName, CancellationToken cancellationToken)
  {

  }

  static async Task<int> HandleEncrypt(string encrypt, string clusterName, CancellationToken cancellationToken)
  {
#pragma warning disable CA1308 // Normalize strings to uppercase
    clusterName = clusterName.ToLowerInvariant();
#pragma warning restore CA1308 // Normalize strings to uppercase
    Console.WriteLine($"🔐 Encrypting '{encrypt}'");
    string masterKeyPath = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age", $"{clusterName}.agekey");
    if (await SOPSCLIWrapper.EncryptAsync(encrypt, masterKeyPath, cancellationToken).ConfigureAwait(false) != 0)
    {
      Console.WriteLine("✕ SOPS encryption failed");
      return 1;
    }
    Console.WriteLine($"✔ '{encrypt}' encrypted");
    return 0;
  }

  static async Task<int> HandleImport(string clusterName, string import, CancellationToken cancellationToken)
  {
#pragma warning disable CA1308 // Normalize strings to uppercase
    clusterName = clusterName.ToLowerInvariant();
#pragma warning restore CA1308 // Normalize strings to uppercase
    string? contents;
    if (File.Exists(import))
    {
      Console.WriteLine($"🔐 Importing SOPS key from '{import}'");
      contents = await File.ReadAllTextAsync(import, cancellationToken).ConfigureAwait(false);
    }
    else
    {
      Console.WriteLine("🔐 Importing SOPS key from stdin");
      contents = import;
    }
    if (!Directory.Exists(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age", $"{clusterName}.agekey")))
    {
      _ = Directory.CreateDirectory(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age"));
    }
    await File.WriteAllTextAsync(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age", $"{clusterName}.agekey"), contents, cancellationToken).ConfigureAwait(false);
    Console.WriteLine($"✔ SOPS key imported to '{Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age", $"{clusterName}.agekey")}'");
    return 0;
  }

  static async Task<int> HandleExport(string clusterName, string export, CancellationToken cancellationToken)
  {
#pragma warning disable CA1308 // Normalize strings to uppercase
    clusterName = clusterName.ToLowerInvariant();
#pragma warning restore CA1308 // Normalize strings to uppercase
    Console.WriteLine($"🔐 Exporting SOPS key to '{export}'");
    if (!File.Exists(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age", $"{clusterName}.agekey")))
    {
      Console.WriteLine("✕ SOPS key not found");
      return 1;
    }
    string contents = await File.ReadAllTextAsync(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".ksail", "age", $"{clusterName}.agekey"), cancellationToken).ConfigureAwait(false);
    await File.WriteAllTextAsync($"{export}/{clusterName}.agekey", contents, cancellationToken).ConfigureAwait(false);
    Console.WriteLine($"✔ SOPS key exported to '{export}'");
    return 0;
  }

  public void Dispose()
  {
    _LocalProvisioner.Dispose();
    GC.SuppressFinalize(this);
  }
}

class FileSearcher
{
  internal static string FindClosestAncestor(string fileName, string directory)
  {
    string currentDirectory = directory;

    while (!string.IsNullOrEmpty(currentDirectory))
    {
      string filePath = Path.Combine(currentDirectory, fileName);
      if (File.Exists(filePath))
      {
        return filePath;
      }
      var parentDirectory = Directory.GetParent(currentDirectory);
      currentDirectory = parentDirectory?.FullName ?? string.Empty;
    }

    throw new FileNotFoundException($"File '{fileName}' not found in any ancestor directories");
  }
}
