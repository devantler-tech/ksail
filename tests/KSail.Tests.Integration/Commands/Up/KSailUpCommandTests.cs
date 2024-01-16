using System.CommandLine;
using System.CommandLine.IO;
using Docker.DotNet;
using Docker.DotNet.Models;
using KSail.CLIWrappers;
using KSail.Commands.Down;
using KSail.Commands.Up;

namespace KSail.Tests.Integration.Commands.Up;

/// <summary>
/// Tests for the <see cref="KSailUpCommand"/> class.
/// </summary>
[UsesVerify]
public class KSailUpCommandTests : IDisposable
{
  private readonly DockerClient dockerClient =
    new DockerClientConfiguration(new Uri("unix:///var/run/docker.sock")).CreateClient();

  /// <summary>
  /// Tests that the <c>ksail up</c> command fails and prints help.
  /// </summary>
  [Fact]
  public async void NoArgsAndOptions_FailsAndPrintsHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailUpCommand = new KSailUpCommand();

    //Act
    int exitCode = await ksailUpCommand.InvokeAsync("", console);

    //Assert
    Assert.Equal(1, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }

  /// <summary>
  /// Tests that the <c>ksail up [name]</c> command fails and prints help.
  /// </summary>
  [Fact]
  public async void NameArg_FailsAndPrintsHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailUpCommand = new KSailUpCommand();

    //Act
    int exitCode = await ksailUpCommand.InvokeAsync("ksail", console);

    //Assert
    Assert.Equal(1, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }

  /// <summary>
  /// Tests that the <c>ksail up [name] --config [config-path] --no-gitops</c> command succeeds and creates a cluster.
  /// </summary>
  [Fact]
  public async void NameArgConfigAndNoGitOps_SucceedsAndCreatesCluster()
  {
    //Arrange
    var ksailUpCommand = new KSailUpCommand();

    //Act
    int exitCode = await ksailUpCommand.InvokeAsync($"ksail --config {Directory.GetCurrentDirectory()}/assets/k3d/k3d-config.yaml --no-gitops", new TestConsole());
    string clusters = await K3dCLIWrapper.ListClustersAsync();
    var containers = await dockerClient.Containers.ListContainersAsync(new ContainersListParameters());

    //Assert
    Assert.Equal(0, exitCode);
    Assert.Contains(containers, container => container.Names.Contains("/proxy-docker.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-registry.k8s.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-gcr.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-ghcr.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-quay.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-mcr.microsoft.com"));
    _ = await Verify(clusters);
  }

  /// <summary>
  /// Tests that the <c>ksail up [name] --config [config-path] --manifests [manifests-path]</c> command succeeds and creates a cluster.
  /// </summary>
  [Fact]
  public async void NameArgConfigAndManifests_SucceedsAndCreatesCluster()
  {
    //Arrange
    var ksailUpCommand = new KSailUpCommand();

    //Act
    int exitCode = await ksailUpCommand.InvokeAsync($"ksail --config {Directory.GetCurrentDirectory()}/assets/k3d/k3d-config.yaml --manifests {Directory.GetCurrentDirectory()}/assets/k8s", new TestConsole());
    string clusters = await K3dCLIWrapper.ListClustersAsync();
    var containers = await dockerClient.Containers.ListContainersAsync(new ContainersListParameters());

    //Assert
    Assert.Equal(0, exitCode);
    Assert.Contains(containers, container => container.Names.Contains("/proxy-docker.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-registry.k8s.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-gcr.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-ghcr.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-quay.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-mcr.microsoft.com"));
    _ = await Verify(clusters);
  }

  /// <summary>
  /// Tests that the <c>ksail up --config [config-path] --no-gitops</c> command succeeds and creates a cluster.
  /// </summary>
  [Fact]
  public async void ConfigAndNoGitOps_SucceedsAndCreatesCluster()
  {
    //Arrange
    var ksailUpCommand = new KSailUpCommand();

    //Act
    int exitCode = await ksailUpCommand.InvokeAsync($"--config {Directory.GetCurrentDirectory()}/assets/k3d/k3d-config.yaml --no-gitops", new TestConsole());
    string clusters = await K3dCLIWrapper.ListClustersAsync();
    var containers = await dockerClient.Containers.ListContainersAsync(new ContainersListParameters());

    //Assert
    Assert.Equal(0, exitCode);
    Assert.Contains(containers, container => container.Names.Contains("/proxy-docker.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-registry.k8s.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-gcr.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-ghcr.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-quay.io"));
    Assert.Contains(containers, container => container.Names.Contains("/proxy-mcr.microsoft.com"));
    _ = await Verify(clusters);
  }

  /// <inheritdoc/>
  public async void Dispose()
  {
    var ksailDownCommand = new KSailDownCommand();
    _ = await ksailDownCommand.InvokeAsync("ksail");

    GC.SuppressFinalize(this);
  }
}
