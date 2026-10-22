#!/usr/bin/env python3
"""Fail a pull request from an agent branch that touches files outside its role.

Usage: check_owned_paths.py <branch-name> <ownership-file> < changed-files.txt

Branches named agent/<role>/... may only change files matching that role's
patterns in the ownership file, and never gate files. Other branches (the human
owner) pass. Exit code 0 = allowed, 1 = blocked, 2 = usage/config error.
"""
import fnmatch
import sys


def load_ownership(path):
    roles = {}
    with open(path, encoding="utf-8") as f:
        for raw in f:
            line = raw.split("#", 1)[0].strip()
            if not line:
                continue
            if ":" not in line:
                raise ValueError(f"bad line in {path}: {raw.rstrip()}")
            role, patterns = line.split(":", 1)
            roles[role.strip()] = patterns.split()
    if "gate" not in roles:
        raise ValueError(f"{path} has no 'gate' line")
    return roles


def matches(path, patterns):
    return any(fnmatch.fnmatchcase(path, p) for p in patterns)


def check(branch, files, roles):
    """Return a list of problems; empty means allowed."""
    if not branch.startswith("agent/"):
        return []
    parts = branch.split("/")
    if len(parts) < 3 or not parts[1]:
        return [f"branch '{branch}' must look like agent/<role>/<issue>-<slug>"]
    role = parts[1]
    if role == "gate" or role not in roles:
        return [f"unknown agent role '{role}' (see .github/ownership.txt)"]
    problems = []
    for path in files:
        if matches(path, roles["gate"]):
            problems.append(f"{path}: gate file, only the owner may change it")
        elif not matches(path, roles[role]):
            problems.append(f"{path}: outside the '{role}' role's folders")
    return problems


def main(argv):
    if len(argv) != 3:
        print(__doc__, file=sys.stderr)
        return 2
    branch, ownership = argv[1], argv[2]
    try:
        roles = load_ownership(ownership)
    except (OSError, ValueError) as e:
        print(f"path-guard config error: {e}", file=sys.stderr)
        return 2
    files = [l.strip() for l in sys.stdin if l.strip()]
    problems = check(branch, files, roles)
    if problems:
        print(f"path-guard: branch {branch} changes files it does not own:")
        for p in problems:
            print(f"  - {p}")
        return 1
    print(f"path-guard: {len(files)} file(s) OK for {branch}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
