using KSail.Utils;

namespace KSail.Tests.Unit.Utils;

public class SopsConfigLoaderTest
{
  [Fact]
  public async Task LoadAsync_NoSopsYamlFile_ThrowsKSailException()
  {
    // Act
    var exception = await Assert.ThrowsAsync<KSailException>(() => SopsConfigLoader.LoadAsync());

    // Assert
    Assert.Equal("'.sops.yaml' file not found in the current or parent directories", exception.Message);
  }
}
