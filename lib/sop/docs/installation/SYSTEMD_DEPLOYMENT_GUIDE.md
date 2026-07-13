# Systemd Deployment Guide

This guide covers the simple, low-cost deployment model for SOP on a single Linux host such as an AWS EC2 instance.

It is split into two workflows:

1. Host deployment for the HTTP server / new site
2. End-user daemon deployment for downloadable binaries distributed via Cloudflare

## 1. Host deployment for the HTTP server / new site

Use this path when you want to run the SOP HTTP server on a server you control, such as an AWS EC2 instance.

### Why this model

- Simple and inexpensive for single-host deployments
- Works well on Ubuntu / Amazon Linux / Debian
- Uses systemd so the service starts automatically after reboot
- No container runtime required

### Build the Linux deployment package

From the repository root:

```bash
cd lib/sop
bash scripts/build-systemd-release.sh
```

This produces a release archive in:

```bash
lib/sop/release/systemd/
```

The archive contains:

- sop-httpserver
- sop-daemon
- install-systemd-service.sh
- install-from-package.sh
- sop-httpserver.service
- sop-daemon.service

### Copy the package to AWS EC2

Example:

```bash
scp lib/sop/release/systemd/sop-systemd-release-linux-amd64.tar.gz ubuntu@YOUR_EC2_HOST:/tmp/
```

### Install on the EC2 host

SSH into the host and run:

```bash
sudo mkdir -p /tmp/sop-install
sudo tar -xzf /tmp/sop-systemd-release-linux-amd64.tar.gz -C /tmp/sop-install
cd /tmp/sop-install
sudo bash install-from-package.sh sop-httpserver 8080 /var/lib/sop
```

This will:

- install the HTTP server binary to /usr/local/bin/sop-httpserver
- create the systemd service unit
- enable the service
- start the service
- ensure it comes back after reboot

### Verify the service

```bash
sudo systemctl status sop-httpserver
sudo journalctl -u sop-httpserver -n 50 --no-pager
```

### Optional: customize the service

If you need a config file:

```bash
sudo bash install-from-package.sh sop-httpserver 8080 /var/lib/sop /etc/sop/config.json
```

### Data directory

Use a persistent filesystem or attached volume for /var/lib/sop so the data survives instance replacement or reboot.

---

## 2. End-user daemon deployment via Cloudflare download

Use this path when you want end users to download a daemon binary from your website and install it on their own machine.

This is the recommended deployment style for the sop-daemon.

### Packaging for end-user download

Build the same Linux deployment package and host the resulting archive from Cloudflare R2, Cloudflare Pages, or another static download endpoint.

Example:

```bash
cd lib/sop
bash scripts/build-systemd-release.sh
```

Then upload the generated archive:

```bash
lib/sop/release/systemd/sop-systemd-release-linux-amd64.tar.gz
```

### Website button flow

On your site, add a button such as:

- Download SOP Daemon

When clicked, the user downloads the archive from Cloudflare.

### Install on the end-user machine

Once downloaded, the user can install it with:

```bash
sudo tar -xzf sop-systemd-release-linux-amd64.tar.gz -C /tmp/sop-install
cd /tmp/sop-install
sudo bash install-from-package.sh sop-daemon 9090 /var/lib/sop
```

This installs the daemon as a systemd service and enables auto-start.

### Verify the daemon service

```bash
sudo systemctl status sop-daemon
sudo journalctl -u sop-daemon -n 50 --no-pager
```

---

## 3. Files used by the deployment flow

The deployment scripts are located in:

- lib/sop/scripts/build-systemd-release.sh
- lib/sop/scripts/install-systemd-service.sh
- lib/sop/scripts/install-from-package.sh
- lib/sop/scripts/systemd/sop-httpserver.service
- lib/sop/scripts/systemd/sop-daemon.service

These are designed for manual deployment and can be invoked from CI/CD later if desired.

---

## 4. Recommended production notes

- Put the service data under /var/lib/sop or another persistent path.
- Use HTTPS in front of the HTTP server where possible.
- Keep secrets in environment variables or a secret manager.
- For AWS, use an attached EBS volume or another durable storage path if the instance data must survive replacement.
- For end-user daemon installs, keep the download package small and clearly versioned.

---

## 5. Quick reference

### AWS EC2 host

```bash
cd lib/sop
bash scripts/build-systemd-release.sh
scp release/systemd/sop-systemd-release-linux-amd64.tar.gz ubuntu@YOUR_EC2_HOST:/tmp/
ssh ubuntu@YOUR_EC2_HOST
sudo mkdir -p /tmp/sop-install
sudo tar -xzf /tmp/sop-systemd-release-linux-amd64.tar.gz -C /tmp/sop-install
cd /tmp/sop-install
sudo bash install-from-package.sh sop-httpserver 8080 /var/lib/sop
```

### End-user daemon download

```bash
sudo tar -xzf sop-systemd-release-linux-amd64.tar.gz -C /tmp/sop-install
cd /tmp/sop-install
sudo bash install-from-package.sh sop-daemon 9090 /var/lib/sop
```
