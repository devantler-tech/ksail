using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationOptions(KSailCluster config)
{
  public ValidationValidateOnUpOption ValidateOnUpOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public ValidationReconcileOnUpOption ReconcileOnUpOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public ValidationValidateOnUpdateOption ValidateOnUpdateOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public ValidationReconcileOnUpdateOption ReconcileOnUpdateOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };
  public ValidationVerboseOption VerboseOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };
}
