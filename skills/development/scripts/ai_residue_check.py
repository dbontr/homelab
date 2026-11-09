#!/usr/bin/env python3
"""Detect assistant-development residue in a repository working tree."""

from __future__ import annotations

import argparse
import fnmatch
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

SKIP_DIRS = {'.git', 'node_modules', '.venv', 'venv', '__pycache__', '.pytest_cache'}
FORBIDDEN_BASENAMES = {
    'agents.md',
    'claude.md',
    'gemini.md',
    '.cursorrules',
    '.clinerules',
}
FORBIDDEN_SEGMENTS = {
    '.agents',
    '.claude',
    '.cursor',
    '.continue',
    '.codex',
    '.roo',
    '.cline',
    '.windsurf',
}
CONTENT_PATTERNS = {
    'AI_ATTRIBUTION': re.compile(
        r'\b(?:generated|written|created)\s+(?:by|with)\s+(?:an?\s+)?(?:ai|chatgpt|claude|copilot|gemini|codex)\b',
        re.I,
    ),
    'AI_ASSISTED': re.compile(r'\bai[- ]assisted\b', re.I),
    'AI_COAUTHOR': re.compile(
        r'^\s*co-authored-by:\s*.*\b(?:chatgpt|openai|claude|anthropic|copilot|gemini|codex)\b',
        re.I | re.M,
    ),
    'ASSISTANT_TRANSCRIPT': re.compile(
        r'\b(?:chatgpt|claude|copilot|gemini|codex)\s+(?:prompt|transcript|conversation)\b',
        re.I,
    ),
}


@dataclass(frozen=True)
class Finding:
    code: str
    path: str
    detail: str


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description='Detect unnecessary AI-development residue in a repository working tree.'
    )
    parser.add_argument('root', nargs='?', default='.', help='Repository root. Default: current directory.')
    parser.add_argument(
        '--allow',
        action='append',
        default=[],
        metavar='GLOB',
        help='Allow one repository-relative path glob. Repeat when necessary.',
    )
    return parser.parse_args()


def git_candidates(root: Path) -> list[Path] | None:
    try:
        probe = subprocess.run(
            ['git', '-C', str(root), 'rev-parse', '--is-inside-work-tree'],
            check=True,
            capture_output=True,
            text=True,
        )
        if probe.stdout.strip() != 'true':
            return None
        listed = subprocess.run(
            ['git', '-C', str(root), 'ls-files', '--cached', '--others', '--exclude-standard', '-z'],
            check=True,
            capture_output=True,
        )
    except (OSError, subprocess.CalledProcessError):
        return None

    return [root / item.decode('utf-8', errors='surrogateescape') for item in listed.stdout.split(b'\0') if item]


def walk_candidates(root: Path) -> list[Path]:
    files: list[Path] = []
    for path in root.rglob('*'):
        if not path.is_file():
            continue
        if any(part in SKIP_DIRS for part in path.relative_to(root).parts):
            continue
        files.append(path)
    return files


def is_allowed(rel: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatch(rel, pattern) for pattern in patterns)


def path_finding(rel: str) -> Finding | None:
    normalized = rel.replace('\\', '/').lower()
    parts = normalized.split('/')
    base = parts[-1]

    if base in FORBIDDEN_BASENAMES:
        return Finding('AI_FILE', rel, f'Assistant-specific file is not permitted: {base}')
    if base.startswith('.aider'):
        return Finding('AI_FILE', rel, 'Aider-specific configuration is not permitted.')
    if any(part in FORBIDDEN_SEGMENTS for part in parts):
        return Finding('AI_PATH', rel, 'Assistant-specific directory is not permitted.')
    if normalized == '.github/copilot-instructions.md':
        return Finding('AI_FILE', rel, 'Copilot instruction file is not permitted.')
    if normalized.startswith('.github/instructions/') and normalized.endswith('.instructions.md'):
        return Finding('AI_FILE', rel, 'Copilot instruction file is not permitted.')
    if normalized.startswith('.github/prompts/') and normalized.endswith('.prompt.md'):
        return Finding('AI_FILE', rel, 'Copilot prompt file is not permitted.')
    return None


def content_findings(path: Path, rel: str) -> list[Finding]:
    try:
        data = path.read_bytes()
    except OSError as exc:
        return [Finding('READ_ERROR', rel, str(exc))]

    if b'\0' in data[:8192]:
        return []
    try:
        text = data.decode('utf-8')
    except UnicodeDecodeError:
        return []
    if len(text) > 2_000_000:
        return []

    findings: list[Finding] = []
    for code, pattern in CONTENT_PATTERNS.items():
        match = pattern.search(text)
        if match:
            snippet = ' '.join(match.group(0).split())[:120]
            findings.append(Finding(code, rel, f'AI-development residue text: {snippet!r}'))
    return findings


def main() -> int:
    args = parse_args()
    root = Path(args.root).resolve()
    if not root.is_dir():
        print(f'ERROR: repository root is not a directory: {root}', file=sys.stderr)
        return 2

    candidates = git_candidates(root) or walk_candidates(root)
    findings: list[Finding] = []
    for path in sorted(set(candidates)):
        try:
            rel = path.relative_to(root).as_posix()
        except ValueError:
            continue
        if is_allowed(rel, args.allow):
            continue
        finding = path_finding(rel)
        if finding:
            findings.append(finding)
            continue
        findings.extend(content_findings(path, rel))

    if not findings:
        print('AI-development residue check: PASS')
        return 0

    for finding in findings:
        print(f'ERROR {finding.code} {finding.path}: {finding.detail}')
    print(f'AI-development residue check: {len(findings)} finding(s)')
    return 1


if __name__ == '__main__':
    raise SystemExit(main())
