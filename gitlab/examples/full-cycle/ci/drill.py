"""Inject one deliberate defect. Never commits, pushes, or modifies a cluster."""
import argparse
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("drill", choices=["unit", "secret", "privileged", "rollout"])
args = parser.parse_args()
if not Path("apps/web/main.go").is_file():
    parser.error("Run from the full-cycle project root")


def replace(path, old, new):
    file = Path(path)
    source = file.read_text()
    if source.count(old) != 1:
        parser.error(f"{path}: expected one healthy baseline; restore the previous drill first")
    file.write_text(source.replace(old, new, 1))


if args.drill == "unit":
    replace("apps/web/main.go", 'json:"version"', 'json:"revision"')
    print("API contract changed: go-tests must reject the missing version field.")
elif args.drill == "secret":
    file = Path("apps/web/drill-credentials.txt")
    if file.exists():
        parser.error("The synthetic credential file already exists")
    # Deliberately fake, assembled so the clean exercise source does not itself contain a token.
    prefix = "gl" + "pat-"
    file.write_text("GITLAB_PAT=" + prefix + "Q4x7T9m2K6v8P3n5R1s0" + "\n")
    print("Synthetic, nonfunctional token added: security-source must reject it.")
elif args.drill == "privileged":
    replace("charts/release-board/values.yaml", "  privileged: false", "  privileged: true")
    print("Privileged app containers enabled: security-config must reject the rendered manifest.")
else:
    replace("charts/release-board/values.yaml", "readinessPath: /readyz", "readinessPath: /not-ready")
    print("A syntactically valid but wrong readiness URL was set. Deployment must time out and roll back.")

print("Inspect git diff. Commit this single drill, record its SHA, then push. Fix with git revert <drill-SHA>.")
