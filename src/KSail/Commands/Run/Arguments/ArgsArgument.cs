using System.CommandLine;

namespace KSail.Commands.Run.Arguments;

class CLIArguments() : Argument<string[]>(
  "args",
  "Arguments to pass to a binary"
);
