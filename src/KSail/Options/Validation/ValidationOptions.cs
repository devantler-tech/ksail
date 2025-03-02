using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Validation;



class ValidationOptions(KSailCluster config)
{


  public ValidationLintOnUpOption LintOnUpOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public ValidationReconcileOnUpOption ReconcileOnUpOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public ValidationLintOnUpdateOption LintOnUpdateOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };


  public ValidationReconcileOnUpdateOption ReconcileOnUpdateOption { get; } = new(config) { Arity = ArgumentArity.ZeroOrOne };

}
