using System.ComponentModel;
using System.Diagnostics;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.Json.Schema;
using System.Text.Json.Serialization;
using System.Text.Json.Serialization.Metadata;
using System.Text.RegularExpressions;
using Devantler.KubernetesGenerator.Core;
using Devantler.KubernetesGenerator.Core.Converters;
using Devantler.KubernetesGenerator.Core.Inspectors;
using KSail.Models;
using KSail.Models.Project.Enums;
using YamlDotNet.Serialization;
using YamlDotNet.Serialization.NamingConventions;
using YamlDotNet.System.Text.Json;

namespace KSail.Docs;


static class SchemaGenerator
{
  public static string Generate()
  {
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
          var jObj = schema.AsObject();
          jObj.Insert(0, "description", descriptionAttr.Description);
        }

        return schema;
      }
    };
    var ksailSchema = options.GetJsonSchemaAsNode(typeof(KSailCluster), exporterOptions);
    foreach (var property in ksailSchema.AsObject())
    {
      if (!schema.ContainsKey(property.Key))
        schema[property.Key] = property.Value?.DeepClone();
    }

    return schema.ToString();
  }
}
