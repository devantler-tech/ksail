using System.CommandLine;

namespace KSail.Commands.Secrets.Arguments;

class PathArgument : Argument<string>
{
  public PathArgument(string description) : base(
    "path"
  ) => Description = description;
}
