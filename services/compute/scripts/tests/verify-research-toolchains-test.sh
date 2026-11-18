#!/usr/bin/env bash
set -euo pipefail

script="$(cd "$(dirname "$0")/.." && pwd)/verify-research-toolchains.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir -p "$scratch/bin"

cat > "$scratch/bin/rustc" <<'TOOL'
#!/usr/bin/env bash
echo "rustc 1.91.1 (test-only)"
TOOL
cat > "$scratch/bin/cargo" <<'TOOL'
#!/usr/bin/env bash
if [[ "${1:-}" == "fmt" || "${1:-}" == "clippy" ]]; then
  echo "cargo-$1 test-only"
else
  echo "cargo 1.91.1 (test-only)"
fi
TOOL
cat > "$scratch/bin/rustup" <<'TOOL'
#!/usr/bin/env bash
if [[ "$*" == "target list --installed" ]]; then
  echo "wasm32-unknown-unknown"
else
  exit 1
fi
TOOL
cat > "$scratch/bin/lean" <<'TOOL'
#!/usr/bin/env bash
echo "Lean (version 4.34.0, test-only)"
TOOL
cat > "$scratch/bin/lake" <<'TOOL'
#!/usr/bin/env bash
echo "Lake version 4.34.0"
TOOL
chmod 755 "$scratch/bin/"*

PATH="$scratch/bin:$PATH" bash "$script" 1.91.1 4.34.0 > "$scratch/result"
grep -Fq 'wasm: wasm32-unknown-unknown' "$scratch/result"
if PATH="$scratch/bin:$PATH" bash "$script" 1.91.1 4.33.0 > "$scratch/bad-version" 2>&1; then
  echo "Incorrect Lean version was accepted" >&2
  exit 1
fi
if PATH="$scratch/bin:$PATH" bash "$script" '1.91.1;touch /tmp/unsafe' 4.34.0 > "$scratch/bad-input" 2>&1; then
  echo "Invalid version input was accepted" >&2
  exit 1
fi
rm "$scratch/bin/lean"
if PATH="$scratch/bin:$PATH" bash "$script" 1.91.1 4.34.0 > "$scratch/missing" 2>&1; then
  echo "Missing Lean tool was accepted" >&2
  exit 1
fi
echo "toolchain preflight positive and negative fixtures passed"
