using System.CommandLine;

class KeyArgument : Argument<string>
{
  public KeyArgument(string description) : base(
    "key"
  ) => Description = description;
}
