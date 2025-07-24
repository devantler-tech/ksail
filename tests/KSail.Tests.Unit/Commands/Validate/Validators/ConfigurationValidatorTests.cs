using System;
using System.CommandLine;
using System.IO;
using System.Threading;
using System.Threading.Tasks;
using DevantlerTech.KubernetesGenerator.K3d.Models;
using DevantlerTech.KubernetesGenerator.Kind.Models;
using KSail.Commands.Root;
using KSail.Commands.Validate.Validators;
using KSail.Models;
using KSail.Models.CNI;
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
  readonly Command _rootCommand;

  public ConfigurationValidatorTest() => _rootCommand = new KSailRootCommand();

  [Fact]
  public async Task ValidateAsync_UnsupportedDistribution_ThrowsNotSupportedException()
  {
    // Arrange
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-unsupported-distribution");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
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
    var exception = await Assert.ThrowsAsync<NotSupportedException>(async () => await validator.ValidateAsync(tempDir, CancellationToken.None).ConfigureAwait(false));
    Assert.Contains("unsupported distribution", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.Kind)]
  [InlineData(KSailDistributionType.K3d)]
  public async Task ValidateAsync_InvalidContextName_ThrowsKSailException(KSailDistributionType distribution)
  {
    // Arrange
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-context-name");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
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
  [InlineData(KSailDistributionType.Kind)]
  [InlineData(KSailDistributionType.K3d)]
  public async Task ValidateAsync_InvalidOCISourceUri_ThrowsKSailException(KSailDistributionType distribution)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-oci-source-uri");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
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
            KSailDistributionType.K3d => "k3d-ksail-default",
            KSailDistributionType.Kind => "kind-ksail-default",
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
  [InlineData(KSailDistributionType.Kind)]
  [InlineData(KSailDistributionType.K3d)]
  public async Task ValidateAsync_InvalidClusterName_ThrowsKSailException(KSailDistributionType distribution)
  {

    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-cluster-name");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
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
            KSailDistributionType.K3d => "k3d.yaml",
            KSailDistributionType.Kind => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3d => "k3d-invalid",
            KSailDistributionType.Kind => "kind-invalid",
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
                KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
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
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Default, KSailCNIType.Cilium)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Cilium, KSailCNIType.Default)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.None, KSailCNIType.Default)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Default, KSailCNIType.None)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Default, KSailCNIType.Cilium)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Cilium, KSailCNIType.Default)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.None, KSailCNIType.Default)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Default, KSailCNIType.None)]
  public async Task ValidateAsync_InvalidCNI_ThrowsKSailException(KSailDistributionType distribution, KSailCNIType actualCNI, KSailCNIType expectedCNI)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-cni");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--cni", expectedCNI.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
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
            KSailDistributionType.K3d => "k3d.yaml",
            KSailDistributionType.Kind => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3d => "k3d-ksail-default",
            KSailDistributionType.Kind => "kind-ksail-default",
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
                KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
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
  [InlineData(KSailDistributionType.K3d, KSailCSIType.None, KSailCSIType.Default)]
  [InlineData(KSailDistributionType.K3d, KSailCSIType.Default, KSailCSIType.None)]
  [InlineData(KSailDistributionType.K3d, KSailCSIType.None, KSailCSIType.LocalPathProvisioner)]
  [InlineData(KSailDistributionType.K3d, KSailCSIType.LocalPathProvisioner, KSailCSIType.None)]
  public async Task ValidateAsync_InvalidCSI_ThrowsKSailException(KSailDistributionType distribution, KSailCSIType actualCSI, KSailCSIType expectedCSI)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-csi");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--csi", expectedCSI.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          CSI = actualCSI,
          DistributionConfigPath = distribution switch
          {
            KSailDistributionType.K3d => "k3d.yaml",
            KSailDistributionType.Kind => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3d => "k3d-ksail-default",
            KSailDistributionType.Kind => "kind-ksail-default",
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
                KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
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
    Assert.Contains($"'spec.project.csi={actualCSI}' in '", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Default, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Default, KSailIngressControllerType.Default)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Default, KSailIngressControllerType.Traefik)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Cilium, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Cilium, KSailIngressControllerType.Default)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.Cilium, KSailIngressControllerType.Traefik)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.None, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.None, KSailIngressControllerType.Default)]
  [InlineData(KSailDistributionType.Kind, KSailCNIType.None, KSailIngressControllerType.Traefik)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Default, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Default, KSailIngressControllerType.Default)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Default, KSailIngressControllerType.Traefik)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Cilium, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Cilium, KSailIngressControllerType.Default)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.Cilium, KSailIngressControllerType.Traefik)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.None, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.None, KSailIngressControllerType.Default)]
  [InlineData(KSailDistributionType.K3d, KSailCNIType.None, KSailIngressControllerType.Traefik)]
  public async Task ValidateAsync_ValidCNIAndIngressController_Succeeds(KSailDistributionType distribution, KSailCNIType cni, KSailIngressControllerType ingressController)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-valid-cni-ingress-controller");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--cni", cni.ToString(), "--ingress-controller", ingressController.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          CNI = cni,
          IngressController = ingressController,
          DistributionConfigPath = distribution switch
          {
            KSailDistributionType.K3d => "k3d.yaml",
            KSailDistributionType.Kind => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3d => "k3d-ksail-default",
            KSailDistributionType.Kind => "kind-ksail-default",
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
                KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
                _ => throw new KSailException($"unsupported distribution '{distribution}'.")
              }
            }
          }
        },
      }
    };
    var validator = new ConfigurationValidator(config);

    // Act
    await validator.ValidateAsync(tempDir, CancellationToken.None);

    // Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.K3d, KSailIngressControllerType.Traefik, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.K3d, KSailIngressControllerType.None, KSailIngressControllerType.Traefik)]
  [InlineData(KSailDistributionType.K3d, KSailIngressControllerType.Default, KSailIngressControllerType.None)]
  [InlineData(KSailDistributionType.K3d, KSailIngressControllerType.None, KSailIngressControllerType.Default)]
  public async Task ValidateAsync_InvalidIngressController_ThrowsKSailException(KSailDistributionType distribution, KSailIngressControllerType actualIngressController, KSailIngressControllerType expectedIngressController)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-ingress-controller");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--ingress-controller", expectedIngressController.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          IngressController = actualIngressController,
          DistributionConfigPath = distribution switch
          {
            KSailDistributionType.K3d => "k3d.yaml",
            KSailDistributionType.Kind => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3d => "k3d-ksail-default",
            KSailDistributionType.Kind => "kind-ksail-default",
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
                KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
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
    Assert.Contains($"'spec.project.ingressController={actualIngressController}' in '", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.K3d, true, false)]
  [InlineData(KSailDistributionType.K3d, false, true)]
  public async Task ValidateAsync_InvalidMetricsServer_ThrowsKSailException(KSailDistributionType distribution, bool actualMetricsServer, bool expectedMetricsServer)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-metrics-server");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--metrics-server", expectedMetricsServer.ToString()]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
    var config = new KSailCluster
    {
      Spec = new KSailClusterSpec
      {
        Project = new KSailProject
        {
          Distribution = distribution,
          MetricsServer = actualMetricsServer,
          DistributionConfigPath = distribution switch
          {
            KSailDistributionType.K3d => "k3d.yaml",
            KSailDistributionType.Kind => "kind.yaml",
            _ => throw new KSailException($"unsupported distribution '{distribution}'.")
          }
        },
        Connection = new KSailConnection
        {
          Context = distribution switch
          {
            KSailDistributionType.K3d => "k3d-ksail-default",
            KSailDistributionType.Kind => "kind-ksail-default",
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
                KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
                KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
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
    Assert.Contains($"'spec.project.metricsServer={actualMetricsServer}' in '", exception.Message, StringComparison.Ordinal);

    //Cleanup
    Directory.Delete(tempDir, true);
  }

  [Theory]
  [InlineData(KSailDistributionType.K3d)]
  public async Task ValidateAsync_InvalidMirrorRegistries_ThrowsKSailException(KSailDistributionType distribution)
  {
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-invalid-mirror-registries");
    var parseResult = _rootCommand.Parse(["init", "--output", tempDir, "--distribution", distribution.ToString(), "--mirror-registries"]);
    using var cts = new CancellationTokenSource();
    _ = await parseResult.InvokeAsync(cts.Token);
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
