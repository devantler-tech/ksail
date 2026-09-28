# 0006: Create EKS clusters natively, and keep managing eksctl-created ones

- Status: Accepted
- Date: 2026-09-25
- Related: #7116, #7117, #7280–#7287

## Context

KSail creates and deletes EKS clusters by running the `eksctl` binary. Users must install it separately, its output is outside KSail's control, and every call it makes needs eksctl's documented minimum IAM policy: `ec2:*`, `eks:*`, `cloudformation:*`, the load-balancing, Auto Scaling and CloudWatch wildcards, and 27 IAM actions on `eksctl-*` names.

Three ways to remove the binary were measured against KSail's own module graph and binary (#7117):

- **(a) Reuse eksctl's CloudFormation template builders** (`pkg/cfn/builder` and `pkg/apis/eksctl.io/v1alpha5`) and deploy the stacks they render.
- **(b) Provision natively** with direct EKS, EC2 and IAM SDK calls, the same way `pkg/client/gke` and `pkg/client/aks` work.
- **(c) Provision natively as in (b), and keep reading and deleting through the `eksctl-<cluster>-*` stacks** of clusters that the eksctl CLI created.

What the measurements showed:

- **Permissions.** CloudFormation creates stack resources with the permissions of the caller, or of a service role when one is passed, so under (a) someone must grant whatever the rendered templates create. Those templates change with eksctl releases and cluster options, which is why eksctl documents its minimum as service wildcards; narrowing that would mean re-deriving the policy from eksctl's templates on every upgrade. Native creation calls each AWS operation directly, so its policy is derived from KSail's own calls: 24 EC2, 7 EKS and 6 IAM operations on the representative path, plus the dependent actions AWS documents for them, such as `iam:PassRole`, `iam:CreateServiceLinkedRole` and `ec2:CreateTags` for tagged creation. Finding and reading eksctl-created clusters needs only read access to CloudFormation (`ListStacks`, `DescribeStacks`). Deleting one through its stacks also needs `cloudformation:DeleteStack` on `eksctl-*` stacks, plus the delete actions for the resources those stacks hold, because CloudFormation deletes them with the caller's permissions too.
- **Coupling.** The builders are not a supported library surface. They call AWS while rendering (`ec2:DescribeInstanceTypeOfferings`), so they cannot render offline, and the templates they produce are not byte-stable: subnet references change order between runs. Managing the stacks they produce means either reimplementing eksctl's stack manager or importing it, and importing it pulls in kops.
- **Size.** Both options are dominated by linking the EC2 client, which alone adds 37.6 MB to the stripped binary. Native adds 37.6 MB in total; the builders with a real EC2 client add 42.8 MB. Size does not decide the choice.
- **Licenses.** Every module either option adds is Apache-2.0, MIT or MIT-0, so neither conflicts with distributing KSail under PolyForm Shield 1.0.0.

## Decision

Choose **(c)**. KSail creates EKS clusters with direct EKS, EC2 and IAM SDK calls, on resource names KSail chooses. It keeps discovering, updating and deleting clusters the eksctl CLI created by reading their `eksctl-<cluster>-*` stacks. Option (a) is rejected, chiefly for its coupling to eksctl internals and for a permission set that follows eksctl's templates rather than KSail's own calls.

Delivery is phased under #7116, with native creation and deletion behind the default-off `experimentalNativeProvisioning` flag until both are proven, then made the default, and only then is the eksctl client removed. The flag chooses how a new cluster is created; deletion chooses its path from how the cluster was actually created, so a natively created cluster is always torn down natively, whatever the flag says later:

| Phase | Issue                                                             |
|-------|-------------------------------------------------------------------|
| 0     | #7280 remove the unused upgrade shim and gate dependency licenses |
| 2     | #7281 validate `eks.yaml` with eksctl's own `v1alpha5` types      |
| 3     | #7282 read clusters and node groups without the binary            |
| 4     | #7283 scale node groups without the binary                        |
| 5     | #7284 create natively behind `experimentalNativeProvisioning`     |
| 5     | #7285 delete natively, including eksctl-created clusters          |
| 6     | #7286 make native the default                                     |
| 7     | #7287 remove the eksctl client                                    |

## Consequences

- **Accepted trade-off:** clusters KSail creates natively carry no eksctl stacks, so the eksctl CLI sees them only as clusters it did not create. Its support for such clusters covers basic operations such as `get cluster`, `scale nodegroup` and `delete cluster`, but not the stack-based management it gives clusters it created itself. The reverse still holds: clusters the eksctl CLI created stay discoverable, updatable and deletable by KSail.
- KSail's IAM policy becomes a list derived from its own calls: each operation it adds brings its action plus any dependent actions AWS documents for it, and no service needs a wildcard. Once native is the default, the CI smoke role in `.github/workflows/system-test-eks.yaml` can drop the CloudFormation create and update permissions and the service wildcards (`ec2:*`, `eks:*`, load balancing, Auto Scaling and CloudWatch). This record does not fix that role's exact policy: resource scoping, the actions AWS only authorizes on every resource, and how the EKS service-linked role and the OIDC provider fit the name prefix and the `eks-ci-smoke-boundary` permissions boundary are designed and tested with #7284 and #7285.
- KSail takes on what eksctl did for it: VPC, subnet, IAM role and OIDC provider creation, and eksctl's delete clean-up. That clean-up covers draining node groups, load balancers left behind by Services, Fargate profiles, add-on and pod-identity IAM, dangling network interfaces and the kubeconfig entry. These costs are sized into #7284 and #7285.
- The permission list in this record comes from the representative code path, not from CloudTrail; no cluster was created while deciding. A full implementation will add calls for add-on roles, the delete clean-up and optional encryption and logging, each named explicitly with its dependent actions.
- `pkg/apis/eksctl.io/v1alpha5` may still be imported for validating `eks.yaml` (#7281), provided the import is measured on its own and stays clear of `pkg/ctl`, `pkg/actions` and kops.

## References

- [eksctl minimum IAM policies](https://eksctl.io/usage/minimum-iam-policies/), the baseline the permission comparison uses.
- The measurements behind this record are on #7117.
