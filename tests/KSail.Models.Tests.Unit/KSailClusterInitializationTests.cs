using Argon;
using KSail.Models.Project.Enums;

namespace KSail.Models.Tests.Unit;


public class KSailClusterInitializationTests
{

  [Fact]
  public async Task InitializeKSailCluster_WithNoInput_ShouldReturnValidConfig()
  {
    // Arrange
    var cluster = new KSailCluster();

    // Act & Assert
    cluster.Spec.DeploymentTool.Flux.Source.Url = new Uri("oci://testhost:5555/ksail-registry");
    var settings = new VerifySettings();
    settings.AddExtraSettings(s => s.DefaultValueHandling = DefaultValueHandling.Include);
    settings.DontIgnoreEmptyCollections();
    _ = await Verify(cluster, settings);
  }


  /// <returns></returns>
  [Fact]
  public async Task InitializeKSailCluster_WithName_ShouldReturnValidConfig()
  {
    // Arrange
    var cluster = new KSailCluster("my-cluster");

    // Act & Assert
    var settings = new VerifySettings();
    settings.AddExtraSettings(s =>
    {
      s.DefaultValueHandling = DefaultValueHandling.Include;
    });
    cluster.Spec.DeploymentTool.Flux.Source.Url = new Uri("oci://testhost:5555/ksail-registry");
    settings.DontIgnoreEmptyCollections();
    _ = await Verify(cluster, settings);
  }


  [Fact]
  public async Task InitializeKSailCluster_WithDistribution_ShouldReturnValidConfig()
  {
    // Arrange
    var cluster = new KSailCluster(KSailDistributionType.K3s);

    // Act & Assert
    var settings = new VerifySettings();
    settings.AddExtraSettings(s =>
    {
      s.DefaultValueHandling = DefaultValueHandling.Include;
    });
    cluster.Spec.DeploymentTool.Flux.Source.Url = new Uri("oci://testhost:5555/ksail-registry");
    settings.DontIgnoreEmptyCollections();
    _ = await Verify(cluster, settings);
  }


  [Fact]
  public async Task InitializeKSailCluster_WithNameAndDistribution_ShouldReturnValidConfig()
  {
    // Arrange
    var cluster = new KSailCluster("my-cluster", KSailDistributionType.K3s);

    // Act & Assert
    var settings = new VerifySettings();
    settings.AddExtraSettings(s =>
    {
      s.DefaultValueHandling = DefaultValueHandling.Include;
    });
    cluster.Spec.DeploymentTool.Flux.Source.Url = new Uri("oci://testhost:5555/ksail-registry");
    settings.DontIgnoreEmptyCollections();
    _ = await Verify(cluster, settings);
  }

  [Fact]
  public async Task InitializeKSailCluster_WithNameAndUnnamedDistribution_ShouldReturnValidConfig()
  {
    // Arrange
    var cluster = new KSailCluster((KSailDistributionType)8);

    // Act & Assert
    var settings = new VerifySettings();
    settings.AddExtraSettings(s =>
    {
      s.DefaultValueHandling = DefaultValueHandling.Include;
    });
    cluster.Spec.DeploymentTool.Flux.Source.Url = new Uri("oci://testhost:5555/ksail-registry");
    settings.DontIgnoreEmptyCollections();
    _ = await Verify(cluster, settings);
  }

  [Fact]
  public async Task InitializeKSailCluster_WithUnnamedDistribution_ShouldReturnValidConfig()
  {
    // Arrange
    var cluster = new KSailCluster("my-cluster", (KSailDistributionType)8);

    // Act & Assert
    var settings = new VerifySettings();
    settings.AddExtraSettings(s =>
    {
      s.DefaultValueHandling = DefaultValueHandling.Include;
    });
    cluster.Spec.DeploymentTool.Flux.Source.Url = new Uri("oci://testhost:5555/ksail-registry");
    settings.DontIgnoreEmptyCollections();
    _ = await Verify(cluster, settings);
  }
}
