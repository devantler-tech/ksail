using System;
using System.CommandLine;
using System.CommandLine.IO;
using System.IO;
using System.Threading;
using System.Threading.Tasks;
using Devantler.KubernetesGenerator.K3d.Models;
using Devantler.KubernetesGenerator.Kind.Models;
using KSail.Commands.Root;
using KSail.Commands.Validate.Validators;
using KSail.Models;
using KSail.Models.Connection;
using KSail.Models.DeploymentTool;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project;
using KSail.Models.Project.Enums;
using Xunit;
using YamlDotNet.Serialization;

namespace KSail.Tests.Unit.Commands.Validate.Validators;

public class ConfigurationValidatorTest
{
  readonly IConsole _console;
  readonly KSailRootCommand _rootCommand;

  public ConfigurationValidatorTest()
  {
    _console = new TestConsole();
    _rootCommand = new KSailRootCommand(_console);
  }

  [Fact]
  public async Task ValidateAsync_UnsupportedDistribution_ThrowsKSailException()
  {
    // Arrange
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-unsupported-distribution");
    _ = await _rootCommand.InvokeAsync(["init", "--output", tempDir], _console);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = (KSailDistributionType)999
        }
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act & Assert
    var exception = await Assert.ThrowsAsync<KSailException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains("unsupported distribution", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.Native)]
  [InlineData(KSailDistributionType.K3s)]
  public async Task ValidateAsync_InvalidContextName_ThrowsKSailException(KSailDistributionType distribution)
  {
    // Arrange
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-context-name");
    _ = await _rootCommand.InvokeAsync(["init", "--output", tempDir, "--distribution", distribution.ToString()], _console);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution
        },
        Connection = new KSailConnection
        {
          Context = "invalid-context"
        }
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act & Assert
    var exception = await Assert.ThrowsAsync<KSailException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains("'config.spec.connection.context' in", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.Native)]
  [InlineData(KSailDistributionType.K3s)]
  public async Task ValidateAsync_InvalidOCISourceUri_ThrowsKSailException(KSailDistributionType distribution)
  {

    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-oci-source-uri");
    _ = await _rootCommand.InvokeAsync(["init", "--output", tempDir, "--distribution", distribution.ToString()], _console);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3s => "k3d-ksail-default",
            KSailDistributionType.Native => "kind-ksail-default",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        DeploymentTool = new KSailDeploymentTool
        {
          Flux = new KSailFluxDeploymentTool
          {
            Source = new KSailFluxDeploymentToolRepository
            {
              Url = new Uri("oci://invalid-oci-source-uri")
            }
          }
        },
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act & Assert
    var exception = await Assert.ThrowsAsync<KSailException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains("'config.spec.deploymentTool.flux.source.url' in", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.Native)]
  [InlineData(KSailDistributionType.K3s)]
  public async Task ValidateAsync_InvalidClusterName_ThrowsKSailException(KSailDistributionType distribution)
  {

    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-cluster-name");
    _ = await _rootCommand.InvokeAsync(["init", "--output", tempDir, "--distribution", distribution.ToString()], _console);
    var config = new KSailCluster
    {
      Metadata = new KSailMetadata
      {
        Name = "invalid"
      },
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          DistributionConfigPath = distribution switch
          {
            KSailDistributionType.K3s => "k3d.yaml",
            KSailDistributionType.Native => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3s => "k3d-invalid",
            KSailDistributionType.Native => "kind-invalid",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        DeploymentTool = new KSailDeploymentTool
        {
          Flux = new KSailFluxDeploymentTool
          {
            Source = new KSailFluxDeploymentToolRepository
            {
              Url = distribution switch
              {
                KSailDistributionType.Native => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3s => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
                _ => throw new KSailException($"unsupported distribution '{distribution}'.")
              }
            }
          }
        },
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act & Assert
    var exception = await Assert.ThrowsAsync<KSailException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains("'metadata.name' in '", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.Native, KSailCNIType.Default, KSailCNIType.Cilium)]
  [InlineData(KSailDistributionType.Native, KSailCNIType.Cilium, KSailCNIType.Default)]
  [InlineData(KSailDistributionType.K3s, KSailCNIType.Default, KSailCNIType.Cilium)]
  [InlineData(KSailDistributionType.K3s, KSailCNIType.Cilium, KSailCNIType.Default)]
  public async Task ValidateAsync_InvalidCNI_ThrowsKSailException(KSailDistributionType distribution, KSailCNIType actualCNI, KSailCNIType expectedCNI)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-cni");
    _ = await _rootCommand.InvokeAsync(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--cni", expectedCNI.ToString()], _console);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          CNI = actualCNI,
          DistributionConfigPath = distribution switch
          {
            KSailDistributionType.K3s => "k3d.yaml",
            KSailDistributionType.Native => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3s => "k3d-ksail-default",
            KSailDistributionType.Native => "kind-ksail-default",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        DeploymentTool = new KSailDeploymentTool
        {
          Flux = new KSailFluxDeploymentTool
          {
            Source = new KSailFluxDeploymentToolRepository
            {
              Url = distribution switch
              {
                KSailDistributionType.Native => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3s => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
                _ => throw new KSailException($"unsupported distribution '{distribution}'.")
              }
            }
          }
        },
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act & Assert
    var exception = await Assert.ThrowsAsync<KSailException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains($"'spec.project.cni={actualCNI}' in '", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.K3s)]
  public async Task ValidateAsync_InvalidMirrorRegistries_ThrowsKSailException(KSailDistributionType distribution)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-mirror-registries");
    _ = await _rootCommand.InvokeAsync(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--mirror-registries"], _console);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          DistributionConfigPath = "k3d.yaml",
          MirrorRegistries = false
        },
        Connection = new KSailConnection
        {
          Context = "k3d-ksail-default"
        },
        DeploymentTool = new KSailDeploymentTool
        {
          Flux = new KSailFluxDeploymentTool
          {
            Source = new KSailFluxDeploymentToolRepository
            {
              Url = new Uri("oci://host.k3d.internal:5555/ksail-registry")
            }
          }
        },
        MirrorRegistries = [
          new KSailMirrorRegistry
          {
            Name = "random.io-proxy",
            HostPort = 7654,
            Proxy = new KSailMirrorRegistryProxy
            {
              Url = new Uri("https://random.io")
            }
          }
        ]
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act & Assert
    var exception = await Assert.ThrowsAsync<KSailException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains("'registries.config' in", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }
}
