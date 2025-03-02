using KSail.Models;

namespace KSail.Utils;


static class KSailClusterExtensions
{
  public static void UpdateConfig<T>(this KSailCluster config, string propertyPath, T value)
  {
    string[] properties = propertyPath.Split('.');
    object? currentObject = config;
    object? defaultObject = new KSailCluster();

    for (int i = 0; i < properties.Length; i++)
    {
      string propertyName = properties[i];
      var property = (currentObject?.GetType().GetProperty(propertyName)) ?? throw new ArgumentException($"Property '{propertyName}' not found on type '{currentObject?.GetType().FullName}'.");
      var defaultProperty = (defaultObject?.GetType().GetProperty(propertyName)) ?? throw new ArgumentException($"Property '{propertyName}' not found on type '{defaultObject?.GetType().FullName}'.");
      if (i == properties.Length - 1)
      {
        // If it's the last property in the path, set the value
        object? currentValue = property.GetValue(currentObject);
        object? defaultValue = defaultProperty.GetValue(defaultObject);

        if (value != null && !Equals(currentValue, value) && !Equals(value, defaultValue))
        {
          if (value is IEnumerable<string> enumerableValue && !enumerableValue.Any())
            continue;
          property.SetValue(currentObject, value);
        }
      }
      else
      {
        // Traverse to the next object in the path
        object? nextObject = property.GetValue(currentObject);
        object? nextDefaultObject = defaultProperty.GetValue(defaultObject);
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
