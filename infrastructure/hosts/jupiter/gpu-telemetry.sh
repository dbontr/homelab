#!/usr/bin/env bash
set -euo pipefail
umask 022

output_dir=/run/homelab-gpu
scratch=$(mktemp "$output_dir/.jupiter-gpu.XXXXXXXX")
trap 'rm -f -- "$scratch"' EXIT

stamp=$(date +%s)
success=0
index='' utilization='' used='' total='' temperature=''

if sample=$(timeout 10s nvidia-smi \
    --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu \
    --format=csv,noheader,nounits 2>/dev/null) &&
   [[ -n $sample && $sample != *$'\n'* ]]; then
  IFS=, read -r index utilization used total temperature <<< "$sample"
  index=${index//[[:space:]]/}
  utilization=${utilization//[[:space:]]/}
  used=${used//[[:space:]]/}
  total=${total//[[:space:]]/}
  temperature=${temperature//[[:space:]]/}
  if [[ $index =~ ^[0-9]+$ && $utilization =~ ^[0-9]+$ &&
        $used =~ ^[0-9]+$ && $total =~ ^[0-9]+$ &&
        $temperature =~ ^[0-9]+$ ]] &&
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
    printf '# HELP homelab_gpu_utilization_percent GPU utilization.\n'
    printf '# TYPE homelab_gpu_utilization_percent gauge\n'
    printf 'homelab_gpu_utilization_percent{node="jupiter"} %d\n' "$utilization"
    printf '# HELP homelab_gpu_memory_used_bytes GPU memory in use.\n'
    printf '# TYPE homelab_gpu_memory_used_bytes gauge\n'
    printf 'homelab_gpu_memory_used_bytes{node="jupiter"} %d\n' "$((10#$used * 1048576))"
    printf '# HELP homelab_gpu_memory_total_bytes GPU memory capacity.\n'
    printf '# TYPE homelab_gpu_memory_total_bytes gauge\n'
    printf 'homelab_gpu_memory_total_bytes{node="jupiter"} %d\n' "$((10#$total * 1048576))"
    printf '# HELP homelab_gpu_temperature_celsius GPU temperature.\n'
    printf '# TYPE homelab_gpu_temperature_celsius gauge\n'
    printf 'homelab_gpu_temperature_celsius{node="jupiter"} %d\n' "$temperature"
  fi
} > "$scratch"
chmod 0644 "$scratch"
mv -f -- "$scratch" "$output_dir/jupiter-gpu.prom"
