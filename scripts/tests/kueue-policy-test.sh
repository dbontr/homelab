#!/usr/bin/env bash
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
helmchart="$root/infrastructure/homelab/kueue/helmchart.yaml"
queues="$root/infrastructure/homelab/kueue/queues.yaml"
verifier="$root/scripts/verify-kueue.sh"
module="$root/infrastructure/homelab/kueue.nix"

bash -n "$verifier"
grep -Fq 'kueue-0.20.6.tgz' "$helmchart"
grep -Eq '^[[:space:]]+manageJobsWithoutQueueName: false$' "$helmchart"
grep -Fq 'homelab.dbontr.me/kueue: enabled' "$helmchart"
grep -Fq -- '- batch/job' "$helmchart"
if grep -Eq -- '- (pod|deployment|statefulset)' "$helmchart"; then
  echo 'Kueue must not manage existing Pod or Deployment lifecycles' >&2
  exit 1
fi
grep -Fq 'name: pwm-batch' "$queues"
grep -Fq 'name: research-cpu' "$queues"
grep -Fq 'kubernetes.io/hostname: venus' "$queues"
grep -Fq 'kubernetes.io/hostname: mercury' "$queues"
grep -Fq 'after = [ "k3s.service" "network-online.target" ]' "$module"
grep -Fq 'lib.mkEnableOption' "$module"
echo 'Kueue admission source policy verified'
