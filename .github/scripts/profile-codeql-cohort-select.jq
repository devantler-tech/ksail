include "profile-codeql-environment";

# Outcomes deliberately never participate in selection. A failed baseline may
# therefore be selected even when another baseline completed successfully.
map({arm:.swapPreparation.requestedAddedSwapGiB,replica,environment:profile_environment}) |
group_by(.environment) | sort_by(.[0].environment) as $cohorts |
[$cohorts[] | select(any(.[]; .arm==0) and any(.[]; .arm==8))] as $eligible |
if ($eligible|length)==0 then
  {policy:"environment-then-lowest-replica-v1",cohorts:$cohorts,baseline:null,treatment:null}
else
  $eligible[0] as $chosen |
  {policy:"environment-then-lowest-replica-v1",cohorts:$cohorts,
    baseline:($chosen|map(select(.arm==0))|sort_by(.replica)|.[0]),
    treatment:($chosen|map(select(.arm==8))|sort_by(.replica)|.[0])}
end
