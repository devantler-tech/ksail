using System.Text.RegularExpressions;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Generator.Tests.KSailClusterGeneratorTests;


public partial class GenerateAsyncTests
{
  readonly KSailClusterGenerator _generator = new();

  [Fact]
  public async Task GenerateAsync_WithPropertiesSet_ShouldGenerateAValidKSailClusterFile()
  {
    // Arrange
    var cluster = new KSailCluster("my-cluster", KSailKubernetesDistributionType.K3s);

    // Act
    string outputPath = Path.Combine(Path.GetTempPath(), "ksail-config.yaml");
    File.Delete(outputPath);
    await _generator.GenerateAsync(cluster, outputPath, true);
    string ksailClusterConfigFromFile = await File.ReadAllTextAsync(outputPath);

    // Assert
    _ = await Verify(ksailClusterConfigFromFile, extension: "yaml")
      .UseFileName("ksail-config.full.yaml")
      .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));

    // Cleanup
    File.Delete(outputPath);
  }


  /// <returns></returns>
  [Fact]
  public async Task GenerateAsync_WithNoPropertiesSet_ShouldGenerateAValidKSailClusterFile()
  {
    // Arrange
    var cluster = new KSailCluster();

    // Act
    string outputPath = Path.Combine(Path.GetTempPath(), "ksail-config.yaml");
    File.Delete(outputPath);
    await _generator.GenerateAsync(cluster, outputPath, true);
    string ksailClusterConfigFromFile = await File.ReadAllTextAsync(outputPath);

    // Assert
    _ = await Verify(ksailClusterConfigFromFile, extension: "yaml")
      .UseFileName("ksail-config.minimal.yaml")
      .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));

    // Cleanup
    File.Delete(outputPath);
  }

  [GeneratedRegex("url:.*")]
  private static partial Regex UrlRegex();
}
