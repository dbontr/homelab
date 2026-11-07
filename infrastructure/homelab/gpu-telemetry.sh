#!/usr/bin/env bash
set -euo pipefail
umask 022

output_dir=/run/homelab-gpu
mkdir -p -- "$output_dir"
scratch=$(mktemp "$output_dir/.jupiter-gpu.XXXXXXXX")
trap 'rm -f -- "$scratch"' EXIT
stamp=$(date +%s)
success=0
sample=''

# Query the GPU workload already scheduled on Jupiter rather than depending
# on an application deployment name. The probe is unavailable when no pod
# has an allocated NVIDIA GPU; report failure instead of stale measurements.
gpu_pod=''
gpu_namespace=''
if pod_list=$(timeout 10s k3s kubectl get pods --all-namespaces -o json 2>/dev/null); then
  gpu_selection=$(jq -er '
    [.items[]
      | select(.status.phase == "Running" and .spec.nodeName == "jupiter")
      | select(any(.spec.containers[];
          ((.resources.limits["nvidia.com/gpu"] //
            .resources.requests["nvidia.com/gpu"] // "0") | tonumber? // 0) > 0))
      | { namespace: .metadata.namespace, name: .metadata.name }]
    | sort_by(.namespace, .name) | .[0] // empty | [.namespace, .name] | @tsv
  ' <<< "$pod_list" 2>/dev/null) || gpu_selection=''
  IFS=$'\t' read -r gpu_namespace gpu_pod <<< "$gpu_selection"
fi

if [[ $gpu_namespace =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] &&
   [[ $gpu_pod =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] &&
    sample=$(timeout 15s k3s kubectl -n "$gpu_namespace" exec "pod/$gpu_pod" -- \
      nvidia-smi --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu \
      --format=csv,noheader,nounits 2>/dev/null) && [[ $sample != *$'\n'* ]]; then
  IFS=, read -r index utilization used total temperature <<< "$sample"
  index=${index//[[:space:]]/}
  utilization=${utilization//[[:space:]]/}
  used=${used//[[:space:]]/}
  total=${total//[[:space:]]/}
  temperature=${temperature//[[:space:]]/}
  if [[ $index =~ ^[0-9]+$ && $utilization =~ ^[0-9]+$ && $used =~ ^[0-9]+$ &&
        $total =~ ^[0-9]+$ && $temperature =~ ^[0-9]+$ ]] &&
      (( utilization <= 100 && used <= total && total > 0 )); then
    success=1
  fi
fi

{
  printf '# HELP homelab_gpu_collection_success Whether the latest GPU sample succeeded.\n'
  printf '# TYPE homelab_gpu_collection_success gauge\n'
  printf 'homelab_gpu_collection_success %d\n' "$success"
  printf '# HELP homelab_gpu_sample_timestamp_seconds Last GPU probe time in Unix seconds.\n'
  printf '# TYPE homelab_gpu_sample_timestamp_seconds gauge\n'
  printf 'homelab_gpu_sample_timestamp_seconds %d\n' "$stamp"
  if (( success )); then
    printf '# HELP homelab_gpu_utilization_percent Jupiter GPU utilization.\n'
    printf '# TYPE homelab_gpu_utilization_percent gauge\n'
    printf 'homelab_gpu_utilization_percent{node="jupiter"} %d\n' "$utilization"
    printf '# HELP homelab_gpu_memory_used_bytes Jupiter GPU memory in use.\n'
    printf '# TYPE homelab_gpu_memory_used_bytes gauge\n'
    printf 'homelab_gpu_memory_used_bytes{node="jupiter"} %d\n' "$((10#$used * 1048576))"
    printf '# HELP homelab_gpu_memory_total_bytes Jupiter GPU memory capacity.\n'
    printf '# TYPE homelab_gpu_memory_total_bytes gauge\n'
    printf 'homelab_gpu_memory_total_bytes{node="jupiter"} %d\n' "$((10#$total * 1048576))"
    printf '# HELP homelab_gpu_temperature_celsius Jupiter GPU temperature.\n'
    printf '# TYPE homelab_gpu_temperature_celsius gauge\n'
    printf 'homelab_gpu_temperature_celsius{node="jupiter"} %d\n' "$temperature"
  fi
} > "$scratch"
chmod 0644 "$scratch"
mv -f -- "$scratch" "$output_dir/jupiter-gpu.prom"
