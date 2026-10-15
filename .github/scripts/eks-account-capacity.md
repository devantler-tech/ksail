# Read regional EKS resource inventories

Run **EKS Account Capacity** manually with an explicit AWS region. The workflow
uses the existing smoke-test identity and makes read-only calls. It creates no
cluster or other AWS resource. The command can also run locally with Go matching
`go.mod`, a current AWS CLI, and existing AWS authentication:

```bash
.github/scripts/report-eks-account-capacity.sh --region eu-central-1
```

The report counts EKS clusters, CloudFormation stacks, EC2 fleets, Spot requests,
instances, Auto Scaling groups, NAT gateways, Elastic IP addresses, network
interfaces, EBS volumes, and Classic and v2 load balancers. It retains the
Standard On-Demand and Spot quota values and anonymized fleet-limit values.
Instances, interfaces and volumes explicitly include AWS-managed resources even
when account visibility hides them. An older CLI that cannot accept that option
produces an unknown observation.

Each resource family has its own complete paginated observation. A failed read,
invalid response, repeated token or resource identity, unsupported state, response
limit or deadline produces `unknown`, never a completed partial count or zero.
Reasons are fixed labels: `access-denied`, `deadline`, `read-failed`, or
`invalid-or-incomplete-data`. Raw AWS errors, names, identifiers, addresses and
page tokens are not printed. State/type groups are capped at 40 lines per kind;
the total includes every observed resource. Each AWS call has a 30-second limit,
and the complete report has a two-minute collection deadline. Resource families
not completed by that deadline remain unknown.

The report's successful exit means the diagnostic ran. It does not mean every
read succeeded, quota is sufficient, or a cleanup passed. Check every kind and
its reason. Usage errors exit 2; a missing runtime or build failure is nonzero.

Regional counts do not establish which resources belong to a particular run.
Deleted stack and NAT records can remain as history. A deleted NAT record does
not establish that its Elastic IP was released. The regional EC2 Fleet listing
omits instant fleets unless their exact IDs are supplied, so their coverage is
always stated as unknown. See [AWS fleet listing](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_DescribeFleets.html)
and [NAT gateway deletion](https://docs.aws.amazon.com/vpc/latest/userguide/nat-gateway-working-with.html#nat-gateway-deleting).

Run-owned capture and post-delete verification remain separate requirements.
This diagnostic cannot retroactively certify an earlier teardown and does not
authorize another billable trial, a permission expansion or a quota increase.

For a separate, fail-closed smoke-candidate quota decision, enable the workflow's
optional check. See [EKS smoke quota admission](eks-capacity-preflight.md).
