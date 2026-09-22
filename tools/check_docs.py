"""Check local Markdown links; not runtime or security verification."""
import re
from pathlib import Path
root = Path(__file__).resolve().parents[1]
errors = []
for path in root.rglob("*.md"):
    if ".git" in path.parts:
        continue
    for target in re.findall(r"\]\(([^)]+)\)", path.read_text()):
        if "://" in target or target.startswith("#"):
            continue
        if not (path.parent / target.split("#")[0]).exists():
            errors.append(f"{path.relative_to(root)}: missing {target}")
if errors:
    raise SystemExit("\n".join(errors))
print("Local Markdown links checked; no implementation or security verification performed.")
