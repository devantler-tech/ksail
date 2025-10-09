using System.CommandLine;
using System.CommandLine.Parsing;
using DevantlerTech.Keys.Age;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Secrets;


public class KSailSecretsCommandTests
{
  readonly Command _ksailCommand = new KSailRootCommand();

  [Theory]
  [InlineData(["secrets", "--help"])]
  [InlineData(["secrets", "encrypt", "--help"])]
  [InlineData(["secrets", "decrypt", "--help"])]
  [InlineData(["secrets", "add", "--help"])]
  [InlineData(["secrets", "rm", "--help"])]
  [InlineData(["secrets", "list", "--help"])]
  [InlineData(["secrets", "import", "--help"])]
  [InlineData(["secrets", "export", "--help"])]
  public async Task KSailSecretsHelp_SucceedsAndPrintsIntroductionAndHelp(params string[] args)
  {
    //Act
    using var cts = new CancellationTokenSource();
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    int exitCode = await _ksailCommand.Parse(args).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString())
      .UseFileName($"ksail {string.Join(" ", args)}");
  }

  [Fact]
  public async Task KSailSecretsAdd_AddsANewEncryptionKeyToSOPSAgeKeyFile()
  {
    //Act
    using var cts = new CancellationTokenSource();
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    int addExitCode = await _ksailCommand.Parse(["secrets", "add"]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string key = outputWriter.ToString().Trim();

    //Assert
    Assert.Equal(0, addExitCode);
    Assert.NotNull(key);
    Assert.NotEmpty(key);

    // Cleanup
    var ageKey = new AgeKey(key);
    int rmExitCode = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    Assert.Equal(0, rmExitCode);
  }

  [Fact]
  public async Task KSailSecretsEncrypt_EncryptsFileContent()
  {
    // Arrange
    string filePath = Path.Combine(Path.GetTempPath(), "testfile.txt");
    string content = "Hello, World!";
    await File.WriteAllTextAsync(filePath, content);

    // Act
    using var cts = new CancellationTokenSource();
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    int addExitCode = await _ksailCommand.Parse(["secrets", "add"]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string key = outputWriter.ToString().Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);
    int encryptExitCode = await _ksailCommand.Parse(["secrets", "encrypt", "--in-place", "--public-key", ageKey.PublicKey, filePath]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string encryptedFileContent = await File.ReadAllTextAsync(filePath);

    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, encryptExitCode);
    Assert.NotEqual(content, encryptedFileContent);

    // Cleanup
    int rmExitCode = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    Assert.Equal(0, rmExitCode);
    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsDecrypt_DecryptsFileContent()
  {
    // Arrange
    using var cts = new CancellationTokenSource();
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    string filePath = Path.Combine(Path.GetTempPath(), "testfile.txt");
    string content = "Hello, World!";
    await File.WriteAllTextAsync(filePath, content);

    // Act
    int addExitCode = await _ksailCommand.Parse(["secrets", "add"]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string key = outputWriter.ToString().Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);
    int encryptExitCode = await _ksailCommand.Parse(["secrets", "encrypt", filePath, "--in-place", "--public-key", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string encryptedFileContent = await File.ReadAllTextAsync(filePath);
    int decryptExitCode = await _ksailCommand.Parse(["secrets", "decrypt", filePath, "--in-place"]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string decryptedFileContent = await File.ReadAllTextAsync(filePath);


    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, encryptExitCode);
    Assert.NotEqual(content, encryptedFileContent);
    Assert.Equal(0, decryptExitCode);
    Assert.Equal(content, decryptedFileContent);

    // Cleanup
    int rmExitCode = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    Assert.Equal(0, rmExitCode);
    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsExport_ExportsAgeKey()
  {
    // Arrange
    using var cts = new CancellationTokenSource();
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    string filePath = Path.Combine(Path.GetTempPath(), "exported_key.txt");
    int addExitCode = await _ksailCommand.Parse(["secrets", "add"]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    Assert.Equal(0, addExitCode);
    string key = outputWriter.ToString().Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);

    // Act
    int exportExitCode = await _ksailCommand.Parse(["secrets", "export", ageKey.PublicKey, "-o", filePath]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    Assert.Equal(0, exportExitCode);
    string exportedKey = await File.ReadAllTextAsync(filePath);

    // Assert
    Assert.NotNull(exportedKey);
    Assert.NotEmpty(exportedKey);
    var exportedAgeKey = new AgeKey(exportedKey);
    Assert.Contains(exportedAgeKey.PublicKey, exportedKey, StringComparison.Ordinal);
    Assert.Contains(exportedAgeKey.PrivateKey, exportedKey, StringComparison.Ordinal);

    // Cleanup
    int rmExitCode = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    Assert.Equal(0, rmExitCode);
    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsImport_ImportsAgeKey()
  {
    // Arrange
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    int addExitCode = await _ksailCommand.Parse(["secrets", "add"]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string key = outputWriter.ToString().Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);
    string filePath = Path.Combine(Path.GetTempPath(), "import_key.txt");

    // Act
    int exportExitCode = await _ksailCommand.Parse(["secrets", "export", ageKey.PublicKey, "-o", filePath]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    string exportedKey = await File.ReadAllTextAsync(filePath);
    int rmExitCode = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    int importExitCode1 = await _ksailCommand.Parse(["secrets", "import", filePath]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    int rmImportedKeyExitCode1 = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    int importExitCode2 = await _ksailCommand.Parse(["secrets", "import", ageKey.ToString()]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);
    int rmImportedKeyExitCode2 = await _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]).InvokeAsync(new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    }, cts.Token);

    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, rmExitCode);
    Assert.Equal(0, exportExitCode);
    Assert.Equal(0, importExitCode1);
    Assert.Equal(0, importExitCode2);
    Assert.NotNull(exportedKey);
    Assert.NotEmpty(exportedKey);
    Assert.Equal(0, rmImportedKeyExitCode1);
    Assert.Equal(0, rmImportedKeyExitCode2);

    // Cleanup

    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsList_ListsKeys()
  {
    // Arrange
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    var parseResult = _ksailCommand.Parse(["secrets", "add"]);
    var invocationConfiguration = new InvocationConfiguration
    {
      Output = outputWriter,
      Error = errorWriter
    };
    using var cts = new CancellationTokenSource();
    int addExitCode = await parseResult.InvokeAsync(invocationConfiguration, cts.Token);
    string key = outputWriter.ToString().Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);

    // Act
    parseResult = _ksailCommand.Parse(["secrets", "list", "--all"]);
    int listExitCode = await parseResult.InvokeAsync(invocationConfiguration, cts.Token);

    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, listExitCode);

    // Cleanup
    parseResult = _ksailCommand.Parse(["secrets", "rm", ageKey.PublicKey]);
    int rmExitCode = await parseResult.InvokeAsync(invocationConfiguration, cts.Token);
    Assert.Equal(0, rmExitCode);
  }
}
