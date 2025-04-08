using System.Linq.Expressions;
using System.Reflection;
using KSail.Models;

namespace KSail.Utils;


static class KSailClusterExtensions
{
  public static void UpdateConfig<T>(this KSailCluster config, Expression<Func<KSailCluster, T>> propertyPathExpression, T value)
  {
    string[] properties = ExtractPropertyPath(propertyPathExpression);
    UpdateNestedProperty(config, properties, value);
  }

  static string[] ExtractPropertyPath<T>(Expression<Func<KSailCluster, T>> propertyPathExpression)
  {
    return [.. propertyPathExpression.Body.ToString()
      .Split('.')
      .Skip(1)
      .Select(p => p.Split(',')[0].Trim())];
  }

  static void UpdateNestedProperty(object currentObject, string[] properties, object? value)
  {
    object? defaultObject = Activator.CreateInstance(currentObject.GetType());

    for (int i = 0; i < properties.Length; i++)
    {
      string propertyName = properties[i];
      var property = GetProperty(currentObject, propertyName);
      var defaultProperty = GetProperty(defaultObject, propertyName);

      if (i == properties.Length - 1)
      {
        UpdateFinalProperty(currentObject, defaultObject, property, defaultProperty, value);
      }
      else
      {
        (currentObject, defaultObject) = UpdateIntermediateProperty(currentObject, defaultObject, property, defaultProperty);
      }
    }
  }

  static PropertyInfo GetProperty(object? obj, string propertyName)
  {
    var property = obj?.GetType().GetProperty(propertyName);
    return property ?? throw new KSailException($"Property '{propertyName}' not found in {obj?.GetType().Name}");
  }

  static void UpdateFinalProperty(object currentObject, object? defaultObject, PropertyInfo property, PropertyInfo defaultProperty, object? value)
  {
    object? currentValue = property.GetValue(currentObject);
    object? defaultValue = defaultProperty.GetValue(defaultObject);

    if (value != null && !Equals(currentValue, value) && !Equals(value, defaultValue))
    {
      if (value is IEnumerable<string> enumerableValue && !enumerableValue.Any())
        return;
      property.SetValue(currentObject, value);
    }
  }

  static (object, object?) UpdateIntermediateProperty(object currentObject, object? defaultObject, PropertyInfo property, PropertyInfo defaultProperty)
  {
    object? nextObject = property.GetValue(currentObject) ?? Activator.CreateInstance(property.PropertyType);
    object? nextDefaultObject = defaultProperty.GetValue(defaultObject) ?? Activator.CreateInstance(defaultProperty.PropertyType);

    property.SetValue(currentObject, nextObject);
    property.SetValue(defaultObject, nextDefaultObject);

    return nextObject == null || nextDefaultObject == null
      ? throw new KSailException($"Property '{property.Name}' not found in {currentObject?.GetType().Name}")
      : ((object, object?))(nextObject, nextDefaultObject);
  }
}
