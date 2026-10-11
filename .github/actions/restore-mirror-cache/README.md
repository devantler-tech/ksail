# Mirror archives for system tests

The Docker CI matrix consumes a validated mirror artifact from its warm job.
Each artifact contains five registry-volume archives, their exact producer key,
and a checksum manifest. Missing identity, incomplete archives, invalid tar data,
or changed checksums stop the restore before cluster creation.

The producer publishes an artifact even when its shared cache already contains
valid archives. Artifacts have one-day retention and a unique name per producer
attempt. Consumers receive the artifact ID through job outputs, so retrying only
a consumer reuses its successful producer's artifact. Rerun the producer as well
if the artifact has expired or was deleted.

The shared mirror cache remains a warm-up optimization. Only default-branch
pushes or dispatches save the large mirror-volume cache; PRs and other refs
still restore reusable entries, then publish their validated run artifact.
A cold PR warms its mirrors once for the whole matrix without retaining a
second multi-gigabyte copy in the repository cache budget. The warm job currently
runs for PRs and opt-in dispatches, so dispatching the full CI suite on the default
branch also refreshes the reusable warming cache.

Optional KWOK and distribution-node image caches are independent. The standalone k3k sentinel
supplies no artifact ID and retains its cache restore and live-pull fallback.
