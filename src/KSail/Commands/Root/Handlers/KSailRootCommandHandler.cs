using System.CommandLine;

namespace KSail.Commands.Root.Handlers;


class KSailRootCommandHandler(ParseResult parseResult) : ICommandHandler
{
  public Task HandleAsync(CancellationToken cancellationToken = default)
  {
    PrintIntroduction();
    return Task.FromResult(0);
  }

  void PrintIntroduction()
  {
    string[] lines =
    [
      @"                    __ ______     _ __",
      @"                   / //_/ __/__ _(_) /",
      @"                  / ,< _\ \/ _ `/ / /",
      @"                 /_/|_/___/\_,_/_/_/",
      @"                                   . . .",
      @"              __/___                 :",
      @"        _____/______|             ___|____     |""\/""|",
      @"_______/_____\_______\_____     ,'        `.    \  /",
      @"\   -----       -\-\-\-    |    |  ^        \___/  |",
      @"~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~",
      @""
    ];

    var outputWriter = parseResult.Configuration.Output;
    Console.ForegroundColor = ConsoleColor.Yellow;
    outputWriter.WriteLine(lines[0]);
    outputWriter.WriteLine(lines[1]);
    outputWriter.WriteLine(lines[2]);
    outputWriter.WriteLine(lines[3]);

    Console.ForegroundColor = ConsoleColor.Blue;
    outputWriter.WriteLine(lines[4]);
    Console.ForegroundColor = ConsoleColor.DarkGreen;
    outputWriter.Write(lines[5][..(lines[5].IndexOf("/__", StringComparison.Ordinal) + 4)]);
    Console.ForegroundColor = ConsoleColor.DarkBlue;
    outputWriter.WriteLine(lines[5][(lines[5].IndexOf("/__", StringComparison.Ordinal) + 4)..]);


    Console.ForegroundColor = ConsoleColor.DarkGreen;
    outputWriter.Write(lines[6][..(lines[6].IndexOf('|', StringComparison.Ordinal) + 1)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    outputWriter.Write(lines[6][(lines[6].IndexOf('|', StringComparison.Ordinal) + 1)..(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 1)]);
    Console.ForegroundColor = ConsoleColor.DarkBlue;
    outputWriter.Write(lines[6][(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 1)..(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 2)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    outputWriter.WriteLine(lines[6][(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 2)..]);

    Console.ForegroundColor = ConsoleColor.DarkGreen;
    outputWriter.Write(lines[7][..lines[7].IndexOf(',', StringComparison.Ordinal)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    outputWriter.WriteLine(lines[7][lines[7].IndexOf(',', StringComparison.Ordinal)..]);


    Console.ForegroundColor = ConsoleColor.DarkGreen;
    outputWriter.Write(lines[8][..lines[8].IndexOf("|  ^", StringComparison.Ordinal)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    outputWriter.WriteLine(lines[8][lines[8].IndexOf("|  ^", StringComparison.Ordinal)..]);

    Console.ForegroundColor = ConsoleColor.DarkBlue;
    outputWriter.WriteLine(lines[9]);
    outputWriter.WriteLine(lines[10]);

    Console.ResetColor();
  }
}
