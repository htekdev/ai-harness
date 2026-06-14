# systemd deployment

Run `harness serve` as a hardened systemd service on any modern Linux host
(Debian/Ubuntu, RHEL/Fedora/Rocky, Arch — anything with systemd ≥ 245).

## Quickstart

```bash
# 1. Install the binary (download from a GitHub Release or build from source).
sudo install -m 0755 ./harness /usr/local/bin/harness

# 2. Create the service user and state directories.
sudo useradd --system --home-dir /var/lib/harness --shell /usr/sbin/nologin harness
sudo install -d -m 0750 -o harness -g harness /var/lib/harness /var/log/harness
sudo install -d -m 0750 -o root    -g harness /etc/harness

# 3. Drop in your harness.md + .harness/ artifacts.
sudo cp -r ./harness.md ./.harness /var/lib/harness/
sudo chown -R harness:harness /var/lib/harness

# 4. Provide credentials.
sudo install -m 0600 -o root -g harness \
  deploy/systemd/harness.env.example /etc/harness/harness.env
sudoedit /etc/harness/harness.env       # paste real keys

# 5. Install and start the unit.
sudo cp deploy/systemd/harness.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now harness

# 6. Tail logs.
journalctl -u harness -f
```

## Config reload

`harness serve` traps `SIGHUP` where supported. To force a clean restart after
editing `harness.md` or files under `.harness/`:

```bash
sudo systemctl reload-or-restart harness
```

## Hardening notes

The unit ships with `NoNewPrivileges`, `ProtectSystem=strict`,
`MemoryDenyWriteExecute`, an empty capability set, and a `@system-service`
syscall filter. These are safe defaults for the static Go binary plus the
in-process tool sandbox (Phase 5.5 network sandbox + Phase 5.9 tool policy).

If your harness uses tools that need broader filesystem access, extend
`ReadWritePaths=` rather than relaxing `ProtectSystem`.

## Uninstall

```bash
sudo systemctl disable --now harness
sudo rm /etc/systemd/system/harness.service /etc/harness/harness.env
sudo systemctl daemon-reload
sudo userdel harness
sudo rm -rf /var/lib/harness /var/log/harness /etc/harness
```
