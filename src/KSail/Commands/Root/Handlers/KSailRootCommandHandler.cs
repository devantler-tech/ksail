using System.CommandLine;

namespace KSail.Commands.Root.Handlers;


class KSailRootCommandHandler() : ICommandHandler
{
  public Task HandleAsync(CancellationToken cancellationToken = default)
  {
    PrintIntroduction();
    return Task.FromResult(0);
  }

  static void PrintIntroduction()
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

    Console.ForegroundColor = ConsoleColor.Yellow;
    Console.WriteLine(lines[0]);
    Console.WriteLine(lines[1]);
    Console.WriteLine(lines[2]);
    Console.WriteLine(lines[3]);

    Console.ForegroundColor = ConsoleColor.Blue;
    Console.WriteLine(lines[4]);
    Console.ForegroundColor = ConsoleColor.DarkGreen;
    Console.Write(lines[5][..(lines[5].IndexOf("/__", StringComparison.Ordinal) + 4)]);
    Console.ForegroundColor = ConsoleColor.DarkBlue;
    Console.WriteLine(lines[5][(lines[5].IndexOf("/__", StringComparison.Ordinal) + 4)..]);


    Console.ForegroundColor = ConsoleColor.DarkGreen;
    Console.Write(lines[6][..(lines[6].IndexOf('|', StringComparison.Ordinal) + 1)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    Console.Write(lines[6][(lines[6].IndexOf('|', StringComparison.Ordinal) + 1)..(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 1)]);
    Console.ForegroundColor = ConsoleColor.DarkBlue;
    Console.Write(lines[6][(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 1)..(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 2)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    Console.WriteLine(lines[6][(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 2)..]);

    Console.ForegroundColor = ConsoleColor.DarkGreen;
    Console.Write(lines[7][..lines[7].IndexOf(',', StringComparison.Ordinal)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    Console.WriteLine(lines[7][lines[7].IndexOf(',', StringComparison.Ordinal)..]);


    Console.ForegroundColor = ConsoleColor.DarkGreen;
    Console.Write(lines[8][..lines[8].IndexOf("|  ^", StringComparison.Ordinal)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    Console.WriteLine(lines[8][lines[8].IndexOf("|  ^", StringComparison.Ordinal)..]);

    Console.ForegroundColor = ConsoleColor.DarkBlue;
    Console.WriteLine(lines[9]);
    Console.WriteLine(lines[10]);

    Console.ResetColor();
  }
}
