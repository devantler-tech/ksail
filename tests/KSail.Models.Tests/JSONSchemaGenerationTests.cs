using System.ComponentModel;
using System.Diagnostics;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Schema;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;

namespace KSail.Models.Tests;


public class KSailClusterJSONSchemaGenerationTests
{
  [Fact]
  public async Task GenerateJSONSchemaFromKSailCluster_ShouldReturnJSONSchema()
  {
    // Arrange & Act
    var options = new JsonSerializerOptions()
    {
      PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
      TypeInfoResolver = new DefaultJsonTypeInfoResolver(),
      Converters = { new JsonStringEnumConverter() }
    };

    var schema = new JsonObject
    {
      ["$schema"] = "https://json-schema.org/draft-07/schema#",
      ["$id"] = "https://raw.githubusercontent.com/devantler/ksail/main/schemas/ksail-cluster-schema.json",
      ["title"] = "KSail Cluster",
      ["description"] = "A configuration object for a KSail cluster"
    };
    var exporterOptions = new JsonSchemaExporterOptions
    {
      TransformSchemaNode = (context, schema) =>
      {
        // Determine if a type or property and extract the relevant attribute provider.
        var attributeProvider = context.PropertyInfo is not null
            ? context.PropertyInfo.AttributeProvider
            : context.TypeInfo.Type;

        // Look up any description attributes.
        var descriptionAttr = attributeProvider?
            .GetCustomAttributes(inherit: true)
            .Select(attr => attr as DescriptionAttribute)
            .FirstOrDefault(attr => attr is not null);

        // Apply description attribute to the generated schema.
        if (descriptionAttr != null)
        {
          if (schema is not JsonObject jObj)
          {
            // Handle the case where the schema is a Boolean.
            var valueKind = schema.GetValueKind();
            Debug.Assert(valueKind is JsonValueKind.True or JsonValueKind.False);
            schema = jObj = [];
            if (valueKind is JsonValueKind.False)
            {
              jObj.Add("not", true);
            }
          }

          jObj.Insert(0, "description", descriptionAttr.Description);
        }

        return schema;
      }
    };
    var ksailSchema = options.GetJsonSchemaAsNode(typeof(KSailCluster), exporterOptions);
    foreach (var property in ksailSchema.AsObject())
    {
      if (!schema.ContainsKey(property.Key))
      {
        schema[property.Key] = property.Value?.DeepClone();
      }
    }

    // Assert
    _ = await Verify(schema.ToString());

    // Write the schema to a file for reference.
    await File.WriteAllTextAsync("../../../../../../schemas/ksail-cluster-schema.json", schema.ToString());
  }
}

