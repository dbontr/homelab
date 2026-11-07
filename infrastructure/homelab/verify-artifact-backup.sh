#!/usr/bin/env bash
set -euo pipefail
umask 077
die() { printf 'Artifact backup verification failed: %s\n' "$*" >&2; exit 1; }
[[ $# == 2 ]] || die 'Provide the extracted task and project artifact roots'
task_root=$1
project_root=$2
for tool in find jq sha256sum stat; do
  command -v "$tool" >/dev/null || die "Missing verification tool: $tool"
done
[[ -d $task_root && ! -L $task_root ]] || die 'Task library was not restored'
[[ -d $project_root && ! -L $project_root ]] || die 'Project artifact library was not restored'

read_manifest() {
  local path=$1
  [[ -f $path && ! -L $path ]] || die 'Missing or symlinked artifact manifest'
  jq -e 'type == "object"' "$path" >/dev/null || die 'Malformed artifact manifest'
}
verify_blob() {
  local folder=$1 revision=${2:-} manifest="$1/manifest.json" data="$1/data"
  [[ -d $folder && ! -L $folder && -f $data && ! -L $data ]] ||
    die 'Artifact data is missing or linked'
  read_manifest "$manifest"
  local expected_id expected_hash expected_bytes actual_hash actual_bytes
  expected_id=$(jq -r '.artifact_id // empty' "$manifest")
  expected_hash=$(jq -r '.sha256 // empty' "$manifest")
  expected_bytes=$(jq -r '.bytes // empty' "$manifest")
  [[ $expected_id == "${folder##*/}" && $expected_hash =~ ^[0-9a-f]{64}$ &&
     $expected_bytes =~ ^[0-9]+$ ]] || die 'Invalid artifact manifest identity'
  actual_hash=$(sha256sum "$data")
  actual_hash=${actual_hash%% *}
  actual_bytes=$(stat -c %s "$data")
  [[ $actual_hash == "$expected_hash" && $actual_bytes == "$expected_bytes" ]] ||
    die 'Artifact digest or size mismatch'
  if [[ -n $revision ]]; then
    jq -e --argjson expected "$revision" '
      .artifact_id == $expected.artifact_id and .name == $expected.name and
      .bytes == $expected.bytes and .sha256 == $expected.sha256
    ' "$manifest" >/dev/null || die 'Task revision differs from artifact'
  fi
}

task_count=0
task_files=0
while IFS= read -r -d '' record; do
  read_manifest "$record"
  task_id=$(jq -r '.task_id // empty' "$record")
  task_directory=${record%/task.json}
  [[ $task_id == "${task_directory##*/}" ]] || die 'Task identifier mismatch'
  jq -e '.artifacts | type == "array"' "$record" >/dev/null ||
    die 'Missing task revision list'
  task_count=$((task_count + 1))
  while IFS= read -r revision; do
    ident=$(jq -r '.artifact_id // empty' <<<"$revision")
    [[ $ident =~ ^[[:alnum:]_-]{32}$ ]] || die 'Invalid task artifact identifier'
    verify_blob "$task_directory/files/task-files/$ident" "$revision"
    task_files=$((task_files + 1))
  done < <(jq -c '.artifacts[]' "$record")
done < <(find "$task_root/owners" -type f -path '*/tasks/task-*/task.json' -print0 2>/dev/null)

project_files=0
while IFS= read -r -d '' manifest; do
  folder=${manifest%/manifest.json}
  verify_blob "$folder"
  project_id=$(jq -r '.project_id // empty' "$manifest")
  project_dir=${folder%/*}
  [[ $project_id == "${project_dir##*/}" ]] || die 'Project artifact owner mismatch'
  project_files=$((project_files + 1))
done < <(find "$project_root" -mindepth 3 -maxdepth 3 -type f -name manifest.json -print0)

jq -nc --argjson tasks "$task_count" --argjson task_files "$task_files" \
  --argjson project_files "$project_files" \
  '{tasks:$tasks, task_files:$task_files, project_files:$project_files}'
