#!/usr/bin/env python3
"""Renew both installation leaf certificates without changing credentials or CA.

Stages and verifies both pairs before replacement. Does not contact Kubernetes;
apply the two TLS Secrets and restart the services using the operations runbook.
"""
import argparse
from datetime import datetime, timezone
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import tempfile


def openssl(*args):
    return subprocess.run(["openssl", *map(str, args)], check=True,
                          capture_output=True, text=True).stdout


def renew(installation, days=90):
    installation = installation.expanduser().resolve(strict=True)
    root = Path(__file__).resolve().parents[1]
    if installation == root or root in installation.parents:
        raise ValueError("installation credentials must remain outside the repository")
    if not 1 <= days <= 90:
        raise ValueError("certificate lifetime must be between 1 and 90 days")
    os.umask(0o077)
    ca = installation / "ca.crt"
    openssl("x509", "-in", ca, "-checkend", days * 86400, "-noout")
    with tempfile.TemporaryDirectory(prefix=".renew-", dir=installation) as temporary:
        staging = Path(temporary)
        for service in ("deck", "marina"):
            current = installation / f"{service}.crt"
            openssl("verify", "-CAfile", ca, current)
            names = openssl("x509", "-in", current, "-noout", "-ext", "subjectAltName")
            names = names.split("\n", 1)[1].strip().replace("IP Address:", "IP:")
            if not names or any(not re.fullmatch(r"(?:DNS:[A-Za-z0-9.*_-]+|IP:[0-9a-fA-F:.]+)",
                                                name.strip()) for name in names.split(",")):
                raise ValueError("unsupported or missing certificate subject alternative names")
            extension = staging / f"{service}.ext"
            extension.write_text(f"subjectAltName={names}\nextendedKeyUsage=serverAuth\n")
            key, csr, certificate = [staging / f"{service}.{suffix}" for suffix in ("key", "csr", "crt")]
            openssl("req", "-newkey", "rsa:3072", "-nodes", "-keyout", key, "-out", csr,
                    "-subj", f"/CN={service}.marina-system.svc")
            openssl("x509", "-req", "-in", csr, "-CA", ca, "-CAkey", installation / "ca.key",
                    "-set_serial", "0x" + secrets.token_hex(16), "-out", certificate,
                    "-days", days, "-sha256", "-extfile", extension)
            openssl("verify", "-x509_strict", "-CAfile", ca, certificate)
        backup = installation / ("certificate-backup-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
                                 + "-" + secrets.token_hex(4))
        backup.mkdir(mode=0o700)
        names = [f"{service}.{suffix}" for service in ("deck", "marina") for suffix in ("crt", "key")]
        for name in names:
            shutil.copy2(installation / name, backup / name)
        try:
            for name in names:
                os.replace(staging / name, installation / name)
        except OSError:
            for name in names:
                shutil.copy2(backup / name, installation / name)
            raise
    return backup


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("installation", type=Path)
    parser.add_argument("--days", type=int, default=90)
    args = parser.parse_args()
    try:
        backup = renew(args.installation, args.days)
    except (OSError, ValueError, subprocess.CalledProcessError):
        parser.exit(1, "Renewal failed; check CA lifetime/key, existing certificates and private-directory permissions.\n")
    print(f"Renewed and verified both leaf certificates; private rollback copies: {backup}")
    print("CA, operator/service identities and credentials preserved. Cluster not changed.")


if __name__ == "__main__":
    main()
