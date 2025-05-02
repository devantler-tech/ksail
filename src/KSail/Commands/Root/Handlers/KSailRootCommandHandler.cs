using System.CommandLine;

namespace KSail.Commands.Root.Handlers;


class KSailRootCommandHandler(IConsole console) : ICommandHandler
{
  public Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    PrintIntroduction(console);
    return Task.FromResult(0);
  }

  static void PrintIntroduction(IConsole console)
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
    console.WriteLine(lines[0]);
    console.WriteLine(lines[1]);
    console.WriteLine(lines[2]);
    console.WriteLine(lines[3]);

    Console.ForegroundColor = ConsoleColor.Blue;
    console.WriteLine(lines[4]);
    Console.ForegroundColor = ConsoleColor.DarkGreen;
    console.Write(lines[5][..(lines[5].IndexOf("/__", StringComparison.Ordinal) + 4)]);
    Console.ForegroundColor = ConsoleColor.DarkBlue;
    console.WriteLine(lines[5][(lines[5].IndexOf("/__", StringComparison.Ordinal) + 4)..]);


    Console.ForegroundColor = ConsoleColor.DarkGreen;
    console.Write(lines[6][..(lines[6].IndexOf('|', StringComparison.Ordinal) + 1)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    console.Write(lines[6][(lines[6].IndexOf('|', StringComparison.Ordinal) + 1)..(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 1)]);
    Console.ForegroundColor = ConsoleColor.DarkBlue;
    console.Write(lines[6][(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 1)..(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 2)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    console.WriteLine(lines[6][(lines[6].IndexOf("_|_", StringComparison.Ordinal) + 2)..]);

    Console.ForegroundColor = ConsoleColor.DarkGreen;
    console.Write(lines[7][..lines[7].IndexOf(',', StringComparison.Ordinal)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    console.WriteLine(lines[7][lines[7].IndexOf(',', StringComparison.Ordinal)..]);


    Console.ForegroundColor = ConsoleColor.DarkGreen;
    console.Write(lines[8][..lines[8].IndexOf("|  ^", StringComparison.Ordinal)]);
    Console.ForegroundColor = ConsoleColor.DarkCyan;
    console.WriteLine(lines[8][lines[8].IndexOf("|  ^", StringComparison.Ordinal)..]);

    Console.ForegroundColor = ConsoleColor.DarkBlue;
    console.WriteLine(lines[9]);
    console.WriteLine(lines[10]);

    Console.ResetColor();
  }
}
