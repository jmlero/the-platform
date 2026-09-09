#!/usr/bin/env python3
"""Install a macOS user LaunchAgent for an existing local Podman/Kind setup."""
import os
from pathlib import Path
import plistlib
import shutil
import subprocess

root = Path(__file__).resolve().parents[1]
state = Path.home() / ".local/share/the-platform/marina-local"
agents = Path.home() / "Library/LaunchAgents"
state.mkdir(parents=True, exist_ok=True, mode=0o700)
agents.mkdir(parents=True, exist_ok=True)
script = state / "start.sh"
shutil.copyfile(root / "scripts/local-podman-start.sh", script)
script.chmod(0o700)
label = "com.theplatform.marina-local"
destination = agents / f"{label}.plist"
podman = shutil.which("podman")
if not podman:
    raise SystemExit("podman is not installed")
configuration = {
    "Label": label,
    "ProgramArguments": ["/bin/sh", str(script)],
    "RunAtLoad": True,
    "AbandonProcessGroup": True,
    "EnvironmentVariables": {"PATH": f"{Path(podman).parent}:/usr/bin:/bin:/usr/sbin:/sbin"},
    "StandardOutPath": str(state / "startup.log"),
    "StandardErrorPath": str(state / "startup.log"),
}
if destination.exists() and plistlib.loads(destination.read_bytes()) != configuration:
    raise SystemExit(f"Refusing to overwrite a different LaunchAgent: {destination}")
destination.write_bytes(plistlib.dumps(configuration))
destination.chmod(0o600)
domain = f"gui/{os.getuid()}"
loaded = subprocess.run(["launchctl", "print", f"{domain}/{label}"], capture_output=True)
if loaded.returncode != 0:
    subprocess.run(["launchctl", "bootstrap", domain, str(destination)], check=True)
else:
    subprocess.run(["launchctl", "kickstart", f"{domain}/{label}"], check=True)
print(f"Installed local startup: {destination}")
