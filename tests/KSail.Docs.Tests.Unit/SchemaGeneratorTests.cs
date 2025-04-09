using System.ComponentModel;
using System.Diagnostics;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Schema;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;

namespace KSail.Docs.Tests.Unit;


public class KSailClusterJSONSchemaGenerationTests
{
  [Fact]
  public async Task GenerateJSONSchemaFromKSailCluster_ShouldReturnJSONSchema()
  {
    // Arrange & Act
    string expectedSchema = SchemaGenerator.Generate();
    string actualSchema = await File.ReadAllTextAsync("../../../../../../schemas/ksail-cluster-schema.json");

    // Assert
    _ = await Verify(expectedSchema.ToString(), extension: "json").UseFileName("ksail-cluster-schema");
    Assert.Equal(expectedSchema, actualSchema);
  }
}

