I've forked to take advantage of the startup PR that wasn't merged from the original (https://github.com/allfro/device-mapping-manager.)


# device-mapping-manager

This maps and enables devices into containers running on docker swarm. It is currently only compatible with linux systems that use cgroup v1 and v2.

## Usage

To run the device-mapping-manager, you can use the following `docker run` command. Note that it requires privileged access and specific volume mounts to interact with the host system (Docker, cgroups, and DBus).

```bash
docker run -d --restart always --name device-manager --privileged \
  --cgroupns=host --pid=host --userns=host \
  -v /sys:/host/sys -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/run/dbus/system_bus_socket:/var/run/dbus/system_bus_socket \
  ghcr.io/gitdeath/device-mapping-manager:master
```

### Requirements

- **Privileged Mode**: Required to manipulate cgroups.
- **Docker Socket**: Required to listen for container events.
- **Cgroups**: Mounted at `/host/sys/fs/cgroup` so the manager can modify device rules.
- **DBus Socket**: Required to listen for `systemd` reload events (to restore mappings after a reload).

## Upgrading

To upgrade from an older version, simply stop and remove the existing container, pull the latest image, and start it again with the new volume mounts:

```bash
# Stop and remove existing container
docker stop device-mapping-manager || true
docker rm device-mapping-manager || true

# Pull latest image
docker pull ghcr.io/gitdeath/device-mapping-manager:master

# Run new version
docker run -d --restart always --name device-manager --privileged \
  --cgroupns=host --pid=host --userns=host \
  -v /sys:/host/sys -v /var/run/docker.sock:/var/run/docker.sock \
  -v /var/run/dbus/system_bus_socket:/var/run/dbus/system_bus_socket \
  ghcr.io/gitdeath/device-mapping-manager:master
```
