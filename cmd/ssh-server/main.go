package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gliderlabs/ssh"
	gossh "golang.org/x/crypto/ssh"
)

func fatal(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {

	ssh.Handle(func(sess ssh.Session) {
		listener := startProxy(sess)
		defer listener.Close()

		_, _, isTty := sess.Pty()
		cfg := &container.Config{
			Image:        "alpine",
			Cmd:          strslice.StrSlice{"sh"},
			Env:          sess.Environ(),
			Tty:          isTty,
			WorkingDir:   "/local",
			OpenStdin:    true,
			AttachStderr: true,
			AttachStdin:  true,
			AttachStdout: true,
			StdinOnce:    true,
			Volumes: map[string]struct{}{
				"/local": struct{}{},
			},
		}
		if len(sess.Command()) > 0 {
			cfg.Cmd = strslice.StrSlice{"sh", "-c", strings.Join(sess.Command(), " ")}
		}
		status, cleanup, err := dockerRun(cfg, sess, listener)
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

func startProxy(sess ssh.Session) net.Listener {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", os.Getenv("PROXY_LISTEN")))
	if err != nil {
		panic(err)
	}
	_, p, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		panic(err)
	}
	log.Printf("starting proxy for %s on port %s...", sess.User(), p)

	go func() {

		for {
			c, err := listener.Accept()
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Println(err)
				return
			}
			log.Println("proxy: connected", c.RemoteAddr())

			go func(in net.Conn) {
				defer in.Close()

				sshConn := sess.Context().Value(ssh.ContextKeyConn).(gossh.Conn)
				channel, reqs, err := sshConn.OpenChannel("localDirFs", nil)
				if err != nil {
					panic(err)
				}
				defer channel.Close()
				go gossh.DiscardRequests(reqs)

				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					io.Copy(in, channel)
					wg.Done()
				}()
				go func() {
					io.Copy(channel, in)
					wg.Done()
				}()
				wg.Wait()

			}(c)
		}
	}()

	return listener
}

func mount(name, port, cwd string) {
	path := fmt.Sprintf("/mnt/sshfs/%s", name)
	os.MkdirAll(path, 0777)
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("sshfs -o allow_other,default_permissions,follow_symlinks,cache=no,workaround=rename:truncate -o StrictHostKeyChecking=no localfs@localhost:%s %s -p %s", cwd, path, port))
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Println(string(out))
	}
	if err != nil {
		log.Println(err)
	}
}

func unmount(name string) {
	path := fmt.Sprintf("/mnt/sshfs/%s", name)
	cmd := exec.Command("fusermount", "-u", path)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		log.Println(string(out))
	}
	if err != nil {
		log.Println(err)
	} else {
		os.RemoveAll(path)
	}
}

func dockerRun(cfg *container.Config, sess ssh.Session, fileserver net.Listener) (status int64, cleanup func(), err error) {
	_, p, err := net.SplitHostPort(fileserver.Addr().String())
	if err != nil {
		panic(err)
	}
	docker, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}
	status = 255
	cleanup = func() {}
	ctx := context.Background()
	sessID := sess.Context().Value(ssh.ContextKeySessionID).(string)

	cwd := "/"
	for _, v := range sess.Environ() {
		parts := strings.SplitN(v, "=", 2)
		if parts[0] == "CWD" {
			cwd = parts[1]
		}
	}

	mount(sessID, p, cwd)
	cleanup = func() {
		unmount(sessID)
	}
	res, err := docker.ContainerCreate(ctx, cfg, &container.HostConfig{
		AutoRemove: true,
		Binds:      []string{fmt.Sprintf("/var/sshfs/%s:/local", sessID)},
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
		unmount(sessID)
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
