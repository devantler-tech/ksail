#!/usr/bin/env bash
# Read the dedicated CI project. This never deletes or classifies ownership.
# Only a complete empty project proves absence without an ownership baseline.
set -uo pipefail
umask 077

if [[ -z "${HCLOUD_TOKEN:-}" || "${HCLOUD_TOKEN}" == *$'\n'* || "${HCLOUD_TOKEN}" == *$'\r'* ]] ||
  ! command -v curl >/dev/null || ! command -v jq >/dev/null; then
  echo 'HETZNER-INVENTORY=UNVERIFIED reason=missing-prerequisite'
  exit 2
fi

inventory_dir="$(mktemp -d)" || exit 2
trap 'rm -rf -- "${inventory_dir}"' EXIT
unverified=0
nonempty=0

read_family() {
  local root="$1" page=1 count=0 page_info page_count next_page total_entries last_page http_status
  local expected_total='' expected_last=''
  local query_args=(--data-urlencode per_page=50)
  [[ "${root}" != images ]] || query_args+=(--data-urlencode type=snapshot --data-urlencode type=backup --data-urlencode include_deprecated=true)
  : >"${inventory_dir}/ids" || return 2
  # This is bounded pagination, not polling. Never follow a response-supplied URL.
  while [[ "${page}" -le 100 ]]; do
    # Keep the credential out of argv and ignore any ambient curl configuration.
    if ! http_status="$(printf 'Authorization: Bearer %s\n' "${HCLOUD_TOKEN}" |
      curl --disable --fail --silent --show-error --proto '=https' --tlsv1.2 \
        --connect-timeout 10 --max-time 20 --header @- --get \
        "${query_args[@]}" --data-urlencode "page=${page}" \
        --url "https://api.hetzner.cloud/v1/${root}" \
        --output "${inventory_dir}/response.json" --write-out '%{http_code}' 2>"${inventory_dir}/error")" ||
      [[ "${http_status}" != 200 ]]; then
      return 2
    fi
    if ! page_info="$(jq -ser --arg root "${root}" --argjson page "${page}" '
      def integer: type == "number" and floor == . and . >= 0 and . <= 9007199254740991;
      if length != 1 then error("multiple responses") else .[0] end |
      if type != "object" or has("error") or (.[$root] | type) != "array"
      then error("invalid resource list") else . end |
      .[$root] as $items | .meta.pagination as $p |
      if ($p | type) != "object" or
        ($p | has("next_page") and has("last_page") and has("total_entries") | not) or
        $p.page != $page or $p.per_page != 50 or ($items | length) > 50 or
        (all($items[]; type == "object" and (.id | integer and . > 0)) | not) or
        ($p.next_page | . == null or (integer and . == ($page + 1)) | not) or
        ($p.last_page | . == null or (integer and . >= $page) | not) or
        ($p.total_entries | . == null or (integer and . >= ($items | length)) | not)
      then error("invalid page")
      else [($items | length), ($p.next_page // 0), ($p.total_entries // -1), ($p.last_page // -1)] | @tsv end
    ' "${inventory_dir}/response.json" 2>/dev/null)"; then
      return 2
    fi
    read -r page_count next_page total_entries last_page <<<"${page_info}"
    if [[ -n "${expected_total}" && "${expected_total}:${expected_last}" != "${total_entries}:${last_page}" ]]; then
      return 2
    fi
    expected_total="${total_entries}" expected_last="${last_page}"
    jq -r --arg root "${root}" '.[$root][].id' "${inventory_dir}/response.json" >>"${inventory_dir}/ids" || return 2
    count=$((count + page_count))
    if [[ "${next_page}" -eq 0 ]]; then
      [[ "${total_entries}" -eq -1 || "${total_entries}" -eq "${count}" ]] || return 2
      [[ "${last_page}" -eq -1 || "${last_page}" -eq "${page}" ]] || return 2
      jq -se 'length == (unique | length)' "${inventory_dir}/ids" >/dev/null || return 2
      printf '%s\n' "${count}"
      return 0
    fi
    page="${next_page}"
  done
  return 2
}

# Include resources created by the provider, CCM, CSI and Talos image uploader.
# Images exclude vendor-supplied system/app images, which are not project assets.
for family in server:servers primary-ip:primary_ips floating-ip:floating_ips volume:volumes load-balancer:load_balancers network:networks firewall:firewalls placement-group:placement_groups ssh-key:ssh_keys image:images; do
  kind="${family%%:*}"
  if ! count="$(read_family "${family#*:}")"; then
    echo "FAMILY ${kind} UNVERIFIED reason=read-or-invalid-response"
    unverified=1
  elif [[ "${count}" -ne 0 ]]; then
    echo "FAMILY ${kind} NONEMPTY count=${count}"
    nonempty=1
  else
    echo "FAMILY ${kind} EMPTY count=0"
  fi
done

if [[ "${unverified}" -ne 0 ]]; then
  echo 'HETZNER-INVENTORY=UNVERIFIED families=10'
  exit 2
fi
if [[ "${nonempty}" -ne 0 ]]; then
  echo 'HETZNER-INVENTORY=NONEMPTY families=10'
  exit 1
fi
echo 'HETZNER-INVENTORY=EMPTY families=10'
