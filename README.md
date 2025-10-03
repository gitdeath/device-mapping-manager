I've forked to take advantage of the startup PR that wasn't merged from the original (https://github.com/allfro/device-mapping-manager.)


# device-mapping-manager

This maps and enables devices into containers running on docker swarm. It is currently only compatible with linux systems that use cgroup v1 and v2.

# Installation

`docker stack deploy -c docker-compose.yml dmm`

# Usage

```yaml
version: "3.8"

services:
  rdesktop:
    image: lscr.io/linuxserver/rdesktop
    volumes:
      - /dev/dri:/dev/dri
    ports:
      - 3389:3389

```
