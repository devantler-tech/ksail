# EKS smoke quota admission

Both manual EKS workflows default to `eu-central-1`. An explicit region input
still selects another region; the applied quota is read there on every check.
The account diagnostic is descriptive. Its successful exit never admits a trial.

The smoke workflow converts the complete generated `eks.yaml` to JSON and runs
`check-eks-capacity.sh` immediately before creation. The supported scenario has
one managed group with one explicit Standard On-Demand instance type and initial
capacities of one. The shared scenario peak also drives the scaling step. Its
two default `t3.medium` target nodes need four default vCPUs. Auto Scaling zone
rebalancing can launch before terminating, exceeding the group maximum by ten
percent or one instance, whichever is greater. The check rounds this allowance
upward and reports target, surge and required vCPUs separately. The current
candidate therefore requires six vCPUs and is refused under the independent
five-vCPU ceiling even when the account quota is higher.

Launch templates, alternative instance selectors, Spot, unmanaged/Fargate/automatic
compute and unfamiliar sizing fields are unsupported and stop as unknown. This
narrow supported shape keeps accounting tied to the actual scenario. KSail's
general project-scaffolding defaults are independent of these CI workflow defaults.

## Accounting and failure behavior

The check reads caller identity, every Service Quotas page, every EC2 instance
page including AWS-managed resources, every Capacity Reservation page, and default
vCPU metadata for the relevant instance types. It uses the exact Standard quota
code `L-1216C47A`. CPU optimization never reduces the default quota charge.

Owned ordinary reservations consume their full capacity in `assessing`,
`scheduled`, `pending`, `active` and `delayed` states, including unused slots
and slots occupied by other accounts. Local running occupants are deduplicated by
their actual reservation association. Reservation ownership is separate from the
account billed for unused capacity. Foreign-owned and special/interruptible
reservations grant no credit and make the observation unknown. Terminal released
reservations are excluded; unfamiliar states remain unknown.

Current usage includes own running Standard On-Demand instances outside those
reservations. Pending instances add a separately labelled conservative buffer.
Stopped and stopping instances are excluded from instance usage; their reservations
can still consume quota. Unresolved shutdown transitions, unsupported families or
tenancy, contradictory associations and missing type metadata stop as unknown.
The Standard classifier requires a Standard family letter followed by a numeric
generation; HPC, Inf, Trn, DL and unfamiliar shapes cannot silently use this quota.

Available headroom is the applied quota minus current usage and the pending
buffer. Admission requires enough headroom for the planned target plus the
documented rebalancing surge allowance. This combined demand must also fit within
five vCPUs. This is a point-in-time
observation, subject to concurrent activity and regional stock availability.
It neither reserves resources nor certifies prior cleanup or authorizes a trial.

Every incomplete, denied, malformed, duplicate, partially failed or timed-out
observation exits nonzero as unknown. No partial total is admitted. The process
boundary permits only fixed read operations, with 30 seconds per call, a two-minute
collection deadline, four MiB per response, 200 pages and 100,000 records per
inventory. At most 100 distinct supported instance types are resolved. Public
output contains only the selected region, aggregate quota/usage/buffer/required
values and fixed reasons. Provider identifiers, raw errors and page tokens stay
out of the report. No permission, quota or capacity fallback is requested.

## Read-only operator check

Run **EKS Account Capacity** with `check_smoke_capacity=true` to evaluate a
candidate with two `t3.medium` nodes without creating AWS resources. This optional
probe uses the same admission implementation; the actual smoke run always checks
its own generated configuration. A refused candidate fails the optional probe
step, while the regional diagnostic's ordinary exit semantics are unchanged.

Locally, with Go matching `go.mod`, AWS CLI and existing authentication:

```bash
yq -o=json '.' eks.yaml | .github/scripts/check-eks-capacity.sh \
  --region eu-central-1 --cluster-name YOUR_CLUSTER --peak-nodes 2
```

AWS references: [On-Demand quotas and instance states](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-on-demand-instances.html),
[CPU optimization](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-optimize-cpu.html),
[reservation quota states](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-capacity-reservations.html),
[shared reservation accounting](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/capacity-reservation-sharing.html),
and [reservation API fields](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_CapacityReservation.html).

[Auto Scaling rebalancing allowance](https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-instance-termination.html#as-instance-termination-availability-zone-rebalancing)
applies to the [Auto Scaling groups managed by EKS](https://docs.aws.amazon.com/eks/latest/userguide/managed-node-groups.html).
The default two-node candidate is deliberately refused until a separately
validated trial design fits the retained ceiling; this change supplies no
instance-size, purchase-option or quota fallback.
