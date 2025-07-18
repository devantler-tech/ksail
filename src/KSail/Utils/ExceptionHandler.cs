using System.CommandLine;
using System.Globalization;
using System.Text;
using CliWrap.Exceptions;

namespace KSail.Utils;


class ExceptionHandler
{

  public bool DebugMode { get; set; }



  public string HandleException(Exception ex)
  {
    if (DebugMode)
    {
      throw ex;
    }
    else
    {
      var message = new StringBuilder();
      message = message.AppendLine(CultureInfo.InvariantCulture, $"✗ {ex.Message}");
      Console.ForegroundColor = ConsoleColor.Red;
      if (ex is not CommandExecutionException)
      {
        Console.WriteLine($"✗ {ex.Message}");
        for (var inner = ex.InnerException; inner is not null; inner = inner.InnerException)
        {
          Console.WriteLine($"  {inner.Message}");
          message = message.AppendLine(CultureInfo.InvariantCulture, $"  {inner.Message}");
        }
      }
      Console.ResetColor();
      return message.ToString();
    }
  }
}
