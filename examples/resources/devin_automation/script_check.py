import json
import os

with open(os.environ["EVENT_FILE"]) as f:
    event = json.load(f)

# Only start a session for high-severity incidents. For webhook:incoming the
# submitted JSON is payload.body.
body = event["payload"].get("body") or {}
run = isinstance(body, dict) and body.get("severity") == "high"
with open(os.environ["OUTPUT_FILE"], "w") as f:
    json.dump({"run": run, "reason": None if run else "severity below high"}, f)
