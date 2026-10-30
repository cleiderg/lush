#!/usr/bin/env python3
"""Block workflow changes that would weaken CI.

Usage: check_workflow_changes.py <base-dir> <head-dir>

Both folders hold the changed files under .github/workflows/, as they are on
main (base) and in the pull request (head). A file missing from head was
deleted; a file missing from base is new. The files are only parsed as data,
never run.

Red flags (any one fails the check):
  - a job is removed or renamed (required checks are job names)
  - a test, lint or build command is removed, or made to always pass (|| true)
  - continue-on-error is turned on for a job or step
  - a job or step is switched off with a literal false condition
  - write permissions are added, a pull_request_target trigger is added,
    or secrets are referenced that weren't before
  - a pull_request_target workflow checks out the pull request's own code

Exit code 0 = allowed, 1 = blocked, 2 = could not read the files.
The owner can let a deliberate change through with the workflow-override label
(the workflow handles that; this script doesn't know about labels).
"""
import os
import re
import sys

import yaml

# Commands whose removal weakens CI. Matched against each line of a step's run script.
CHECK_CMD = re.compile(
    r"\b(go\s+(test|vet|build)|gofmt|npm\s+(test|run\s+(lint|build|test))|npx\s+(vitest|eslint|tsc)|"
    r"make\s+(test|lint|build|smoke|protocol|deploy-check|loadtest)|shellcheck|actionlint|"
    r"check_[a-z_]+\.py|git\s+diff\s+--exit-code|exit\s+1)\b")
# Package installs mention tool names ("apt-get install shellcheck") but aren't checks.
INSTALL = re.compile(r"\b(apt(-get)?|brew|pip3?|npm|yarn|pnpm)\b[^|;&]*?\b(install|ci|add)\b")
# Ways to make a failing command pass anyway.
NEUTRALIZED = re.compile(r"(\|\|\s*(true|:|exit\s+0)\b|;\s*(true|exit\s+0)\s*$)")
FALSE_CONDITION = re.compile(r"^\s*(\$\{\{\s*)?(false|0|''|\"\")(\s*\}\})?\s*$", re.IGNORECASE)


def load(path):
    with open(path, encoding="utf-8") as f:
        data = yaml.safe_load(f)
    return data if isinstance(data, dict) else {}


def triggers(wf):
    # YAML 1.1 reads the key `on` as the boolean True.
    on = wf.get("on", wf.get(True))
    if isinstance(on, str):
        return {on}
    if isinstance(on, list):
        return set(on)
    if isinstance(on, dict):
        return set(on)
    return set()


def jobs(wf):
    j = wf.get("jobs")
    return j if isinstance(j, dict) else {}


def steps(job):
    s = job.get("steps") if isinstance(job, dict) else None
    return [x for x in s if isinstance(x, dict)] if isinstance(s, list) else []


def check_lines(job):
    """Every test/lint/build command line in a job's run scripts, whitespace-normalized."""
    out = []
    for st in steps(job):
        for line in str(st.get("run") or "").splitlines():
            line = " ".join(line.split())
            if line and not line.startswith("#") and CHECK_CMD.search(line) and not INSTALL.search(line):
                out.append(line)
    return out


def is_on(value):
    return value is True or (isinstance(value, str) and value.strip().lower() in ("true", "${{ true }}"))


def writes(perms):
    if isinstance(perms, str):
        return {"*"} if perms.strip() == "write-all" else set()
    if isinstance(perms, dict):
        return {k for k, v in perms.items() if str(v).strip() == "write"}
    return set()


def all_writes(wf):
    out = {("workflow", p) for p in writes(wf.get("permissions"))}
    for name, job in jobs(wf).items():
        if isinstance(job, dict):
            out |= {(name, p) for p in writes(job.get("permissions"))}
    return out


def secrets_used(text):
    return set(re.findall(r"secrets\.([A-Za-z0-9_]+)", text))


def checks_out_pr_code(wf):
    for job in jobs(wf).values():
        for st in steps(job):
            if str(st.get("uses", "")).startswith("actions/checkout"):
                ref = str((st.get("with") or {}).get("ref", ""))
                repo = str((st.get("with") or {}).get("repository", ""))
                if "pull_request.head" in ref or "head_ref" in ref or "pull_request.head" in repo:
                    return True
    return False


def compare(name, base_text, head_text):
    """Problems for one workflow file. base_text or head_text is None when the file is new or deleted."""
    problems = []
    if head_text is None:
        return ["%s: the workflow file is deleted" % name]
    try:
        head = yaml.safe_load(head_text)
        head = head if isinstance(head, dict) else {}
    except yaml.YAMLError as e:
        return ["%s: not valid YAML (%s)" % (name, str(e).splitlines()[0])]
    base = {}
    if base_text is not None:
        try:
            base = yaml.safe_load(base_text)
            base = base if isinstance(base, dict) else {}
        except yaml.YAMLError:
            base = {}
    base_jobs, head_jobs = jobs(base), jobs(head)

    for job_name in base_jobs:
        if job_name not in head_jobs:
            problems.append("%s: job '%s' is removed or renamed (required checks use job names)" % (name, job_name))

    for job_name, job in head_jobs.items():
        if not isinstance(job, dict):
            continue
        old = base_jobs.get(job_name) if isinstance(base_jobs.get(job_name), dict) else {}
        if is_on(job.get("continue-on-error")) and not is_on(old.get("continue-on-error")):
            problems.append("%s: job '%s' now has continue-on-error" % (name, job_name))
        if FALSE_CONDITION.match(str(job.get("if", "x"))) and not FALSE_CONDITION.match(str(old.get("if", "x"))):
            problems.append("%s: job '%s' is switched off with a false condition" % (name, job_name))
        old_steps = {str(s.get("name") or s.get("run") or s.get("uses")): s for s in steps(old)}
        for st in steps(job):
            label = str(st.get("name") or st.get("run") or st.get("uses"))
            prev = old_steps.get(label, {})
            if is_on(st.get("continue-on-error")) and not is_on(prev.get("continue-on-error")):
                problems.append("%s: step '%s' in job '%s' now has continue-on-error" % (name, label[:60], job_name))
            if FALSE_CONDITION.match(str(st.get("if", "x"))) and not FALSE_CONDITION.match(str(prev.get("if", "x"))):
                problems.append("%s: step '%s' in job '%s' is switched off with a false condition" % (
                    name, label[:60], job_name))
        head_lines = check_lines(job)
        for line in check_lines(old):
            if line not in head_lines:
                problems.append("%s: job '%s' no longer runs: %s" % (name, job_name, line[:100]))
        for line in head_lines:
            if NEUTRALIZED.search(line) and line not in check_lines(old):
                problems.append("%s: job '%s' makes a check always pass: %s" % (name, job_name, line[:100]))

    added_writes = all_writes(head) - all_writes(base)
    for scope, perm in sorted(added_writes):
        problems.append("%s: new write permission '%s' for %s" % (name, perm, scope))
    if "pull_request_target" in triggers(head) and "pull_request_target" not in triggers(base):
        problems.append("%s: adds a pull_request_target trigger" % name)
    new_secrets = secrets_used(head_text) - secrets_used(base_text or "")
    if new_secrets:
        problems.append("%s: uses new secrets: %s" % (name, ", ".join(sorted(new_secrets))))
    if "pull_request_target" in triggers(head) and checks_out_pr_code(head):
        problems.append("%s: a pull_request_target workflow checks out the pull request's code" % name)
    return problems


def read(path):
    if not os.path.isfile(path):
        return None
    with open(path, encoding="utf-8") as f:
        return f.read()


def main(argv):
    if len(argv) != 3:
        print(__doc__)
        return 2
    base_dir, head_dir = argv[1], argv[2]
    names = set()
    for d in (base_dir, head_dir):
        for root, _, files in os.walk(d):
            for f in files:
                names.add(os.path.relpath(os.path.join(root, f), d))
    if not names:
        print("No workflow files changed.")
        return 0
    problems = []
    for n in sorted(names):
        problems += compare(n, read(os.path.join(base_dir, n)), read(os.path.join(head_dir, n)))
    if problems:
        print("Blocked: this pull request weakens CI.")
        for p in problems:
            print("  - " + p)
        print("If the change is deliberate, the owner can add the workflow-override label.")
        return 1
    print("OK: %d workflow file(s) changed; no checks removed, skipped or loosened." % len(names))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
