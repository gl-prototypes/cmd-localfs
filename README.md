# localfs-docker-ssh
Remotely sharing a local directory with a container over SSH

## Setting up

You need Docker (developed with Docker for Mac) with the [docker-9p plugin](https://github.com/progrium/docker-9p)
installed and enabled.

## Running the server

```
$ LOCAL9P_HOST=192.168.1.208 go run cmd/ssh-server/main.go
```

LOCAL9P_HOST needs to be set to your machine's IP so the Docker for Mac VM
can connect to this server.

## Running the client

```
$ go run cmd/ssh-client/main.go localhost:2222
```

This connects to the SSH server above while also starting a 9P server on `:5640`.
In theory the 9P Docker volume plugin will use `mount` against the server on
your `LOCAL9P_HOST` at 5641, which will proxy the session to the client here.
