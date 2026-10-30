"""Tests for check_workflow_changes.py. Run: python3 -m unittest scripts/ci/test_check_workflow_changes.py"""
import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_workflow_changes as C  # noqa: E402

BASE = """name: ci
on:
  pull_request:
  push:
    branches: [main]
permissions:
  contents: read
jobs:
  server:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: vet
        run: go vet ./...
      - name: test
        working-directory: server
        run: go test -race ./...
  client:
    runs-on: ubuntu-latest
    steps:
      - name: lint
        run: npm run lint
      - name: test
        run: npm test -- --run
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Install caddy
        run: |
          sudo apt-get update
          sudo apt-get install -y caddy shellcheck
      - name: Deploy config checks
        run: make deploy-check
"""


def change(old, new, text=BASE):
    assert old in text, old
    return text.replace(old, new, 1)


class Compare(unittest.TestCase):
    def problems(self, head, base=BASE):
        return C.compare("ci.yml", base, head)

    def test_unchanged_passes(self):
        self.assertEqual(self.problems(BASE), [])

    def test_harmless_install_change_passes(self):
        head = change("          sudo apt-get update\n          sudo apt-get install -y caddy shellcheck\n",
                      "          sudo apt-get -o Acquire::Retries=3 install -y --no-install-recommends caddy\n")
        head = change("  deploy:\n    runs-on: ubuntu-latest\n", "  deploy:\n    runs-on: ubuntu-latest\n    timeout-minutes: 15\n",
                      head)
        self.assertEqual(self.problems(head), [])

    def test_new_job_passes(self):
        head = BASE + "  docs:\n    runs-on: ubuntu-latest\n    steps:\n      - run: make lint\n"
        self.assertEqual(self.problems(head), [])

    def test_removed_job(self):
        head = BASE.split("  client:")[0] + "  deploy:" + BASE.split("  deploy:")[1]
        self.assertTrue(any("job 'client' is removed" in p for p in self.problems(head)))

    def test_renamed_job(self):
        self.assertTrue(any("job 'server' is removed" in p for p in self.problems(change("  server:", "  backend:"))))

    def test_removed_test_command(self):
        head = change("      - name: test\n        working-directory: server\n        run: go test -race ./...\n", "")
        self.assertTrue(any("no longer runs: go test -race ./..." in p for p in self.problems(head)))

    def test_changed_test_command_counts_as_removed(self):
        head = change("run: go test -race ./...", "run: go test ./internal/world/...")
        self.assertTrue(any("no longer runs" in p for p in self.problems(head)))

    def test_neutralized_command(self):
        head = change("run: npm run lint", "run: npm run lint || true")
        probs = self.problems(head)
        self.assertTrue(any("always pass" in p for p in probs))

    def test_continue_on_error_on_step(self):
        head = change("      - name: lint\n", "      - name: lint\n        continue-on-error: true\n")
        self.assertTrue(any("continue-on-error" in p for p in self.problems(head)))

    def test_continue_on_error_on_job(self):
        head = change("  client:\n    runs-on: ubuntu-latest\n", "  client:\n    runs-on: ubuntu-latest\n    continue-on-error: true\n")
        self.assertTrue(any("job 'client' now has continue-on-error" in p for p in self.problems(head)))

    def test_false_condition(self):
        head = change("      - name: vet\n", "      - name: vet\n        if: ${{ false }}\n")
        self.assertTrue(any("switched off" in p for p in self.problems(head)))
        head = change("  deploy:\n    runs-on: ubuntu-latest\n", "  deploy:\n    if: false\n    runs-on: ubuntu-latest\n")
        self.assertTrue(any("job 'deploy' is switched off" in p for p in self.problems(head)))

    def test_real_conditions_pass(self):
        head = change("      - name: vet\n", "      - name: vet\n        if: steps.has.outputs.yes == 'true'\n")
        self.assertEqual(self.problems(head), [])

    def test_write_permission(self):
        self.assertTrue(any("new write permission 'contents'" in p
                            for p in self.problems(change("  contents: read", "  contents: write"))))
        self.assertTrue(any("write-all" in p or "'*'" in p
                            for p in self.problems(change("permissions:\n  contents: read", "permissions: write-all"))))

    def test_pull_request_target(self):
        head = change("  pull_request:\n", "  pull_request_target:\n")
        self.assertTrue(any("pull_request_target" in p for p in self.problems(head)))

    def test_checkout_of_pr_code_in_pull_request_target(self):
        base = change("  pull_request:\n", "  pull_request_target:\n")
        head = change("      - uses: actions/checkout@v4\n",
                      "      - uses: actions/checkout@v4\n        with:\n          ref: ${{ github.event.pull_request.head.sha }}\n",
                      base)
        self.assertTrue(any("checks out the pull request's code" in p for p in self.problems(head, base)))

    def test_new_secret(self):
        head = change("        run: make deploy-check\n",
                      "        run: make deploy-check\n        env:\n          T: ${{ secrets.DEPLOY_KEY }}\n")
        self.assertTrue(any("new secrets: DEPLOY_KEY" in p for p in self.problems(head)))

    def test_deleted_file(self):
        self.assertTrue(any("deleted" in p for p in C.compare("ci.yml", BASE, None)))

    def test_new_file_with_writes_is_flagged(self):
        new = "on: push\npermissions:\n  contents: write\njobs:\n  x:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"
        self.assertTrue(any("write permission" in p for p in C.compare("new.yml", None, new)))

    def test_invalid_yaml(self):
        self.assertTrue(any("not valid YAML" in p for p in C.compare("ci.yml", BASE, "jobs: [\n")))


class Cli(unittest.TestCase):
    def run_cli(self, base_files, head_files):
        with tempfile.TemporaryDirectory() as b, tempfile.TemporaryDirectory() as h:
            for d, files in ((b, base_files), (h, head_files)):
                for name, text in files.items():
                    with open(os.path.join(d, name), "w", encoding="utf-8") as f:
                        f.write(text)
            script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "check_workflow_changes.py")
            return subprocess.run([sys.executable, script, b, h], capture_output=True, text=True)

    def test_ok(self):
        r = self.run_cli({"ci.yml": BASE}, {"ci.yml": BASE})
        self.assertEqual(r.returncode, 0, r.stdout)

    def test_blocked(self):
        r = self.run_cli({"ci.yml": BASE}, {"ci.yml": change("run: npm run lint", "run: echo skipped")})
        self.assertEqual(r.returncode, 1)
        self.assertIn("no longer runs: npm run lint", r.stdout)

    def test_nothing_changed(self):
        r = self.run_cli({}, {})
        self.assertEqual(r.returncode, 0)


if __name__ == "__main__":
    unittest.main()
