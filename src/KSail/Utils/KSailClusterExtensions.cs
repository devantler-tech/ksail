using System.Linq.Expressions;
using KSail.Models;

namespace KSail.Utils;


static class KSailClusterExtensions
{
  public static void UpdateConfig<T>(this KSailCluster config, Expression<Func<KSailCluster, T>> propertyPathExpression, T value)
  {
    string[] properties = [.. propertyPathExpression.Body.ToString().Split('.').Skip(1).Select(p => p.Split(',')[0].Trim())];
    object? currentObject = config;
    object? defaultObject = new KSailCluster();

    for (int i = 0; i < properties.Length; i++)
    {
      string propertyName = properties[i];
      var property = currentObject?.GetType().GetProperty(propertyName);
      var defaultProperty = defaultObject?.GetType().GetProperty(propertyName);
      if (i == properties.Length - 1)
      {
        object? currentValue = property?.GetValue(currentObject);
        object? defaultValue = defaultProperty?.GetValue(defaultObject);

        if (value != null && !Equals(currentValue, value) && !Equals(value, defaultValue))
        {
          if (value is IEnumerable<string> enumerableValue && !enumerableValue.Any())
            continue;
          property?.SetValue(currentObject, value);
        }
      }
      else
      {
        object? nextObject = property?.GetValue(currentObject);
        object? nextDefaultObject = defaultProperty?.GetValue(defaultObject);
        if (property == null || defaultProperty == null)
        {
          throw new KSailException($"Property '{propertyName}' not found in {currentObject?.GetType().Name}");
        }
        nextObject ??= Activator.CreateInstance(property.PropertyType);
        nextDefaultObject ??= Activator.CreateInstance(defaultProperty.PropertyType);
        property.SetValue(currentObject, nextObject);
        property.SetValue(defaultObject, nextDefaultObject);
        currentObject = nextObject;
        defaultObject = nextDefaultObject;
      }
    }
  }
}
