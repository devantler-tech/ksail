using System.CommandLine;

namespace KSail.Commands.Secrets.Arguments;

class PublicKeyArgument : Argument<string>
{
  public PublicKeyArgument(string description) : base(
    "public-key"
  ) => Description = description;
}
