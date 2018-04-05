# cmd-localfs
Prototype of remote commands with access to local directory

## Setting up

You need Docker (developed with Docker for Mac).

## Build and run the server

The server daemon needs to run in Docker. You can build and run it with make:

```
$ make build run
```

## Running the client

To quickly test, you can connect using `make client`. This is slightly boring since
you'll always be in this project directory. Build and install the client and use it from ANY directory.

```
$ go install ./cmd/ssh-client/...
$ ssh-client localhost:2222
...
```

Now try running the server on a remote machine and use the same client with it. It's currently
hardcoded to always use the `alpine` image as the command environment. This defeats the point
a little, but isn't the point of this prototype.

## Limitations

Not sure the best way to configure sshfs (you can see what I approximate as the right options
in the code) and I'm not sure how much of this problem is the SFTP server implementation BUT:

Any files you create in the `/local` mount will be created but not written to and return an error.
For example, `touch` does make the file but returns an error that it couldn't. Using `mv` doesn't work.
You can, however, `touch` a file, ignore the error, then pipe data into the file with output
redirection for example.  
