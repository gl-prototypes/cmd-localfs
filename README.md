# localfs-docker-ssh
Remotely sharing a local directory with a container over SSH

## Setting up

You need Docker (developed with Docker for Mac) with the [docker-9p plugin](https://github.com/progrium/docker-9p)
installed and enabled.

## Running the server

```
$ 9P_HOST=192.168.1.208 go run cmd/ssh-server/main.go
```

9P_HOST needs to be set to your machine's IP so the Docker for Mac VM
can connect to this server.

## Running the client

```
$ go run cmd/ssh-client/main.go localhost:2222
```

This connects to the SSH server above and accepts and serves 9P connections
over the SSH connection that serve up the current directory. The Docker volume
plugin is used by the server as it creates a volume for your SSH connection pointing
to itself that will proxy the 9P connection to your client over SSH.

## Development Notes

This is a proof of concept. There are panics all over. And the [go-p9p](https://github.com/progrium/go-p9p) library
is I think still the best out there with our changes, but it's still a pretty
alpha library.

A current bug with this is that when Linux mounts the 9P filesystem tunneled
through everything, it will first connect and then fail, causing this error on
the client:
```
2018/01/19 16:51:19 9p: serve: error reading fcall: unexpected EOF
```
Then something waits for a connection to close before it retries and this time
it works. Normally it would happen faster, but our proxying isn't very fancy.
The real solution would be to find out why [Linux's 9P client](https://landley.net/kdocs/Documentation/filesystems/9p.txt) needs to connect
twice.

I discovered it does try to connect using 9P version "9P2000.L", so I figured
it was retrying once it realized it needed to talk just "9P2000". But even
after making the 9P Docker Volume plugin use the "noextend" option, which forces
9P2000, it still needs to connect twice.

This would definitely need to be solved before this could be shaped into anything
for production, so any help on figuring this out would be appreciated!
