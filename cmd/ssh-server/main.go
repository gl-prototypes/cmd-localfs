package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	p9p "github.com/docker/go-p9p"
	"github.com/gliderlabs/ssh"
)

func main() {
	listener, err := net.Listen("tcp", ":5641")
	if err != nil {
		panic(err)
	}
	go proxy9p(listener)
	log.Println("starting 9p proxy on port 5641...")
	defer listener.Close()

	ssh.Handle(func(sess ssh.Session) {
		_, _, isTty := sess.Pty()
		cfg := &container.Config{
			Image:        "alpine",
			Cmd:          strslice.StrSlice{"sh"},
			Env:          sess.Environ(),
			Tty:          isTty,
			OpenStdin:    true,
			AttachStderr: true,
			AttachStdin:  true,
			AttachStdout: true,
			StdinOnce:    true,
			Volumes: map[string]struct{}{
				"/mnt": struct{}{},
			},
		}
		status, cleanup, err := dockerRun(cfg, sess)
		defer cleanup()
		if err != nil {
			fmt.Fprintln(sess, err)
			log.Println(err)
		}
		sess.Exit(int(status))
	})

	log.Println("starting ssh server on port 2222...")
	log.Fatal(ssh.ListenAndServe(":2222", nil))
}

func proxy9p(listener net.Listener) {
	for {
		c, err := listener.Accept()
		if err != nil {
			log.Println("9p: accept:", err)
			continue
		}
		log.Println("proxy: new conn")

		go func(conn net.Conn) {
			defer conn.Close()

			ctx := context.Background()

			backend, err := net.Dial("tcp", "127.0.0.1:5640")
			if err != nil {
				log.Println("9p: dial:", err)
				return
			}

			session, err := p9p.NewSession(ctx, backend)
			if err != nil {
				log.Println("9p: session:", err)
				return
			}

			if err := p9p.ServeConn(ctx, conn, p9p.Dispatch(session)); err != nil {
				if err != io.EOF {
					log.Println("9p: serve:", err)
				}
			}
		}(c)
	}
}

func dockerRun(cfg *container.Config, sess ssh.Session) (status int64, cleanup func(), err error) {
	docker, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}
	status = 255
	cleanup = func() {}
	ctx := context.Background()

	v, err := docker.VolumeCreate(ctx, volume.VolumesCreateBody{
		Driver: "progrium/docker-9p",
		DriverOpts: map[string]string{
			"host": os.Getenv("LOCAL9P_HOST"),
			"port": "5641",
		},
	})
	if err != nil {
		return
	}
	cleanup = func() {
		docker.VolumeRemove(ctx, v.Name, true)
	}
	res, err := docker.ContainerCreate(ctx, cfg, &container.HostConfig{
		AutoRemove: true,
		Binds:      []string{fmt.Sprintf("%s:/mnt", v.Name)},
	}, nil, "")
	if err != nil {
		return
	}
	opts := types.ContainerAttachOptions{
		Stdin:  cfg.AttachStdin,
		Stdout: cfg.AttachStdout,
		Stderr: cfg.AttachStderr,
		Stream: true,
	}
	stream, err := docker.ContainerAttach(ctx, res.ID, opts)
	if err != nil {
		return
	}
	cleanup = func() {
		docker.VolumeRemove(ctx, v.Name, true)
		stream.Close()
	}

	outputErr := make(chan error)

	go func() {
		var err error
		if cfg.Tty {
			_, err = io.Copy(sess, stream.Reader)
		} else {
			_, err = stdcopy.StdCopy(sess, sess.Stderr(), stream.Reader)
		}
		outputErr <- err
	}()

	go func() {
		defer stream.CloseWrite()
		io.Copy(stream.Conn, sess)
	}()

	err = docker.ContainerStart(ctx, res.ID, types.ContainerStartOptions{})
	if err != nil {
		return
	}
	if cfg.Tty {
		_, winCh, _ := sess.Pty()
		go func() {
			for win := range winCh {
				err := docker.ContainerResize(ctx, res.ID, types.ResizeOptions{
					Height: uint(win.Height),
					Width:  uint(win.Width),
				})
				if err != nil {
					log.Println(err)
					break
				}
			}
		}()
	}
	resultC, errC := docker.ContainerWait(ctx, res.ID, container.WaitConditionNotRunning)
	select {
	case err = <-errC:
		return
	case result := <-resultC:
		status = result.StatusCode
	}
	err = <-outputErr
	return
}
