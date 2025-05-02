using System.CommandLine;
using System.CommandLine.Builder;
using System.CommandLine.IO;
using System.CommandLine.Parsing;
using Devantler.Keys.Age;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Secrets;


public class KSailSecretsCommandTests
{
  readonly TestConsole _console;
  readonly Parser _ksailCommand;
  public KSailSecretsCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new CommandLineBuilder(new KSailRootCommand(_console))
      .UseVersionOption()
      .UseHelp("--helpz")
      .UseEnvironmentVariableDirective()
      .UseParseDirective()
      .UseSuggestDirective()
      .RegisterWithDotnetSuggest()
      .UseTypoCorrections()
      .UseParseErrorReporting()
      .UseExceptionHandler()
      .CancelOnProcessTermination()
      .Build();
  }

  [Theory]
  [InlineData(["secrets", "--helpz"])]
  [InlineData(["secrets", "encrypt", "--helpz"])]
  [InlineData(["secrets", "decrypt", "--helpz"])]
  [InlineData(["secrets", "add", "--helpz"])]
  [InlineData(["secrets", "rm", "--helpz"])]
  [InlineData(["secrets", "list", "--helpz"])]
  [InlineData(["secrets", "import", "--helpz"])]
  [InlineData(["secrets", "export", "--helpz"])]
  public async Task KSailSecretsHelp_SucceedsAndPrintsIntroductionAndHelp(params string[] args)
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(args, _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out)
      .UseFileName($"ksail {string.Join(" ", args)}");
  }

  [Fact]
  public async Task KSailSecretsAdd_AddsANewEncryptionKeyToSOPSAgeKeyFile()
  {
    //Act
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();

    //Assert
    Assert.Equal(0, addExitCode);
    Assert.NotNull(key);
    Assert.NotEmpty(key);

    // Cleanup
    var ageKey = new AgeKey(key);
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
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
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);
    int encryptExitCode = await _ksailCommand.InvokeAsync(["secrets", "encrypt", "--in-place", "--public-key", ageKey.PublicKey, filePath], _console);
    string encryptedFileContent = await File.ReadAllTextAsync(filePath);

    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, encryptExitCode);
    Assert.NotEqual(content, encryptedFileContent);

    // Cleanup
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    Assert.Equal(0, rmExitCode);
    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsDecrypt_DecryptsFileContent()
  {
    // Arrange
    string filePath = Path.Combine(Path.GetTempPath(), "testfile.txt");
    string content = "Hello, World!";
    await File.WriteAllTextAsync(filePath, content);

    // Act
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);
    int encryptExitCode = await _ksailCommand.InvokeAsync(["secrets", "encrypt", filePath, "--in-place", "--public-key", ageKey.PublicKey], _console);
    string encryptedFileContent = await File.ReadAllTextAsync(filePath);
    int decryptExitCode = await _ksailCommand.InvokeAsync(["secrets", "decrypt", filePath, "--in-place"], _console);
    string decryptedFileContent = await File.ReadAllTextAsync(filePath);


    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, encryptExitCode);
    Assert.NotEqual(content, encryptedFileContent);
    Assert.Equal(0, decryptExitCode);
    Assert.Equal(content, decryptedFileContent);

    // Cleanup
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    Assert.Equal(0, rmExitCode);
    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsExport_ExportsAgeKey()
  {
    // Arrange
    string filePath = Path.Combine(Path.GetTempPath(), "exported_key.txt");
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);

    // Act
    int exportExitCode = await _ksailCommand.InvokeAsync(["secrets", "export", ageKey.PublicKey, "-o", filePath], _console);
    string exportedKey = await File.ReadAllTextAsync(filePath);

    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, exportExitCode);
    Assert.NotNull(exportedKey);
    Assert.NotEmpty(exportedKey);
    var exportedAgeKey = new AgeKey(exportedKey);
    Assert.Contains(exportedAgeKey.PublicKey, exportedKey, StringComparison.Ordinal);
    Assert.Contains(exportedAgeKey.PrivateKey, exportedKey, StringComparison.Ordinal);

    // Cleanup
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    Assert.Equal(0, rmExitCode);
    File.Delete(filePath);
  }

  [Fact]
  public async Task KSailSecretsImport_ImportsAgeKey()
  {
    // Arrange
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);
    string filePath = Path.Combine(Path.GetTempPath(), "import_key.txt");

    // Act
    int exportExitCode = await _ksailCommand.InvokeAsync(["secrets", "export", ageKey.PublicKey, "-o", filePath], _console);
    string exportedKey = await File.ReadAllTextAsync(filePath);
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    int importExitCode1 = await _ksailCommand.InvokeAsync(["secrets", "import", filePath], _console);
    int rmImportedKeyExitCode1 = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    int importExitCode2 = await _ksailCommand.InvokeAsync(["secrets", "import", ageKey.ToString()], _console);
    int rmImportedKeyExitCode2 = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);

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
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();
    Assert.NotNull(key);
    Assert.NotEmpty(key);
    var ageKey = new AgeKey(key);

    // Act
    int listExitCode = await _ksailCommand.InvokeAsync(["secrets", "list", "--all"], _console);

    // Assert
    Assert.Equal(0, addExitCode);
    Assert.Equal(0, listExitCode);

    // Cleanup
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    Assert.Equal(0, rmExitCode);
  }
}
