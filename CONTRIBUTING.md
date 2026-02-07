# Contribution guide

This document describes **development-only** setup options for contributing to the x-ui project.
Production deployments are **out of scope** for this guide.

For safety, Docker-based development is **strongly recommended**.  
Local installation should be performed **only inside a VM or disposable environment**.

---

## Recommended: Docker development setup

### Prerequisites

- Docker
- Docker Compose (v2+)
- Git

### Steps

```bash
git clone https://github.com/alireza0/x-ui.git
# or:
# git clone https://github.com/<your_fork_name>/x-ui.git

docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build
```

### Explore panel

- Panel should be available at localhost:54321 like: [http://127.0.0.1:54321](http://127.0.0.1:54321) for example
- Default credentials: `admin` / `admin`

To stop and remove containers or `ctrl` + `C`:

```bash
docker compose down
```

### Environment

When using the Docker development setup, the [docker-compose.dev.yml](/docker-compose.dev.yml) file mounts the local database directory into the container:

```yaml
volumes:
  - ./dev/db:/etc/x-ui
```

This means:

- [./dev/db](/dev/db/) on the host is used to persist panel state during development
- `/etc/x-ui` inside the container is the active database/config directory
- Data will survive container restarts but is isolated from the host system
- You can safely delete `./dev/db` to reset the development database

---

## Local development setup (unsafe on host OS)

This method runs scripts with root privileges and alters the host system.
Use **only inside a VM, container, or disposable test environment**.

### Prerequisites

- Ubuntu/Debian or Fedora (actually anything with systemd, just use proper package manager)

### Install dependencies

```bash
# Ubuntu / Debian
sudo apt update
sudo apt upgrade -y
sudo apt install -y golang unzip git wget

# Fedora
sudo dnf update -y
sudo dnf install -y golang unzip git wget
```

### Setup panel

```bash
git clone https://github.com/alireza0/x-ui.git
# or:
# git clone https://github.com/<your_fork_name>/x-ui.git

cd x-ui/
sudo bash x-ui.sh   # Select -> 10. Start
```

### Explore panel

- Panel should be available at localhost:54321 like: [http://127.0.0.1:54321](http://127.0.0.1:54321) for example
- Default credentials: `admin` / `admin`
