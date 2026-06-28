# SOP Cheap HA Operations Package

This package provides a simple, installable failover helper for running SOP on two hosts in a low-cost high-availability setup.

## What it does

The helper can be installed on both servers and will:
- monitor the peer host,
- detect when the peer is unhealthy,
- promote the local host to active when needed,
- start the SOP service on the backup host,
- and expose simple status and control commands.

This is intended as a practical starting point for a POC or small production deployment, not a full replacement for mature clustering tools such as Pacemaker, Keepalived with HAProxy, or Kubernetes.

## Requirements

Before installing, ensure each target host has:
- a Linux distribution with systemd (recommended),
- SSH access between the two hosts,
- SSH keys configured for passwordless login from the backup host to the primary host and vice versa,
- a service name for SOP that can be managed by systemctl or service,
- enough permissions to install files under /usr/local/bin and /etc/systemd/system.

## Initial beta-site setup vision

This setup is well suited for an initial beta deployment on a low-cost hosting platform with two mid-level servers in an active/passive arrangement.

Why it fits the beta phase:
- SOP database redundancy can be configured with one data shard and one parity shard, so the system can continue operating even if one server fails.
- In an active/passive model, the backup server can take over and continue serving requests in a reduced mode until the failed node is restored.
- This gives high availability without forcing the business to buy expensive dedicated redundancy hardware or enterprise clustering software.
- Self-registered users, subscription plans, and authentication/authorization workloads are lightweight enough that two mid-level servers are sufficient at this stage.
- Each user can use their own host for local AI/LLM sessions, while the central server mainly handles secure authentication and authorization.

This is a practical, budget-friendly deployment model for early rollout and validation.

## Local autonomy and bundled UI

For better autonomy, the daemon app should ship with its own bundled UI so users do not need to sign in to a central HQ portal just to access the application experience.

Recommended approach:
- Bundle the UI directly with the daemon app so it can be launched locally from the agent or desktop experience.
- Keep the central server primarily responsible for secure authentication, authorization, and shared coordination.
- Let users relaunch the app locally without depending on HQ availability for basic access.
- Let the UI connect directly to the local agent so users can continue working even if the browser session is interrupted.
- Preserve local continuity if the HQ hosting site or HQ server goes down, so work can continue on the local runtime without a hard dependency on central availability.

This reduces friction for end users and makes the beta deployment feel more self-contained and resilient.

## Installation

Run the installer as root on each server:

```bash
cd /path/to/sop/operations/ha
chmod +x install.sh sop-ha-failover.sh
sudo ./install.sh
```

The installer will:
- copy the failover helper to /usr/local/bin/sop-ha,
- install a systemd unit to /etc/systemd/system/sop-ha.service,
- create a configuration file at /etc/sop-ha/sop-ha.conf if one does not already exist.

## Configuration

Edit the configuration file on each server.

```bash
sudo nano /etc/sop-ha/sop-ha.conf
```

Example:

```bash
SERVICE_NAME=sop
PEER_HOST=backup-host
PEER_USER=root
POLL_INTERVAL=10
FAIL_TIMEOUT=30
```

Set the values as follows:
- SERVICE_NAME: the systemd or service name for your SOP process.
- PEER_HOST: the hostname or IP of the other server.
- PEER_USER: the SSH user that can connect to the peer.
- POLL_INTERVAL: seconds between health checks.
- FAIL_TIMEOUT: seconds without a healthy peer before local promotion occurs.

## Step-by-step setup

### 1. Prepare SSH access

Make sure both hosts can reach each other over SSH without a password prompt.

```bash
ssh-copy-id root@backup-host
ssh-copy-id root@primary-host
```

### 2. Install on the primary server

```bash
sudo ./install.sh
sudo nano /etc/sop-ha/sop-ha.conf
```

Set PEER_HOST to the backup server hostname or IP.

### 3. Install on the backup server

```bash
sudo ./install.sh
sudo nano /etc/sop-ha/sop-ha.conf
```

Set PEER_HOST to the primary server hostname or IP.

### 4. Start the service

On both hosts:

```bash
sudo systemctl daemon-reload
sudo systemctl enable sop-ha.service
sudo systemctl start sop-ha.service
```

### 5. Verify operation

Check status on each server:

```bash
sudo /usr/local/bin/sop-ha status
```

You should see a current state such as active or passive.

## Manual commands

You can also control the helper manually:

```bash
sudo /usr/local/bin/sop-ha promote
sudo /usr/local/bin/sop-ha demote
sudo /usr/local/bin/sop-ha status
```

## Notes

- This helper is intentionally simple and easy to deploy.
- It is best used together with a load balancer, VIP, or DNS-based traffic switch.
- For production-grade failover, combine this with a stronger HA stack if required.
