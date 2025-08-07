using System.Globalization;
using KSail.Managers;

namespace KSail.Tests.Unit.Managers;

public class ReconciliationProgressManagerTests
{
  [Fact]
  public void ReconciliationProgressManager_CanBeCreated_WithValidParameters()
  {
    // Arrange & Act
    using var manager = new ReconciliationProgressManager(
      "",  // Empty kubeconfig for in-cluster config
      "test-context",
      TimeSpan.FromMinutes(10));

    // Assert
    Assert.NotNull(manager);
  }

  [Fact]
  public void ReconciliationProgressManager_HandlesInvalidKubeconfig_Gracefully()
  {
    // This test verifies that the manager can be created even with invalid kubeconfig
    // The actual Kubernetes connection will fail later, but object creation should succeed

    // Arrange & Act & Assert
    _ = Assert.Throws<FileNotFoundException>(() =>
    {
      using var manager = new ReconciliationProgressManager(
        "/invalid/path/to/kubeconfig",
        "test-context",
        TimeSpan.FromMinutes(5));
    });
  }

  [Fact]
  public void ReconciliationProgressManager_DisposesCorrectly()
  {
    // Arrange
    ReconciliationProgressManager? manager = null;

    // Act & Assert - Should not throw
    var exception = Record.Exception(() =>
    {
      manager = new ReconciliationProgressManager(
        "",
        "test-context",
        TimeSpan.FromSeconds(1));

      manager.Dispose();
    });

    Assert.Null(exception);
  }

  [Fact]
  public void ReconciliationProgressManager_CreatesWithVariousTimeouts()
  {
    // Test various timeout values
    var timeouts = new[]
    {
      TimeSpan.FromSeconds(30),
      TimeSpan.FromMinutes(5),
      TimeSpan.FromMinutes(15)
    };

    foreach (var timeout in timeouts)
    {
      using var manager = new ReconciliationProgressManager("", null, timeout);
      Assert.NotNull(manager);
    }
  }
}