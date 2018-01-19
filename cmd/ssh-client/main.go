package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"strings"
	"time"

	p9p "github.com/progrium/go-p9p"
	"github.com/progrium/go-p9p/ufs"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

func tryRSA(usr *user.User) ssh.Signer {
	key, err := ioutil.ReadFile(fmt.Sprintf("%s/.ssh/id_rsa", usr.HomeDir))
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil
	}
	return signer
}

func tryDSA(usr *user.User) ssh.Signer {
	key, err := ioutil.ReadFile(fmt.Sprintf("%s/.ssh/id_dsa", usr.HomeDir))
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil
	}
	return signer
}

func main() {
	flag.Parse()

	u, err := user.Current()
	if err != nil {
		panic(err)
	}

	config := &ssh.ClientConfig{
		User: u.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(
				tryRSA(u),
				tryDSA(u),
			),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := flag.Arg(0)
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "22"
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(host, port), config)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	fsChan := client.HandleChannelOpen("localDirFs")
	go func() {
		for newChan := range fsChan {
			ch, reqs, err := newChan.Accept()
			if err != nil {
				panic(err)
			}
			go ssh.DiscardRequests(reqs)

			ctx := context.Background()
			session, err := ufs.NewSession(ctx, dir)
			if err != nil {
				log.Println("9p: session:", err)
				return
			}

			if err := p9p.ServeConn(ctx, &virtualConn{ch}, p9p.Dispatch(session)); err != nil {
				if err != io.EOF {
					log.Println("9p: serve:", err)
				}
			}
		}
	}()

	session, err := client.NewSession()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	cmd := flag.Args()[1:]
	if len(cmd) > 2 && cmd[0] == "--" {
		cmd = cmd[1:]
	}

	if len(cmd) > 0 {
		if err := session.Start(strings.Join(cmd, " ")); err != nil {
			panic(err)
		}
	} else {
		modes := ssh.TerminalModes{
		// ssh.ECHO:          0,     // disable echoing
		// ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		// ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}
		term := os.Getenv("TERM")
		if term == "" {
			term = "xterm"
		}
		width, height, err := terminal.GetSize(0)
		if err != nil {
			panic(err)
		}
		if err := session.RequestPty(term, height, width, modes); err != nil {
			panic(err)
		}
		oldState, err := terminal.MakeRaw(0)
		if err != nil {
			panic(err)
		}
		defer terminal.Restore(0, oldState)
		// TODO: handle SIGWINCH
		if err := session.Shell(); err != nil {
			terminal.Restore(0, oldState)
			panic(err)
		}
	}

	if err := session.Wait(); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			//terminal.Restore(0, oldState)
			os.Exit(exitErr.ExitStatus())
		} else {
			//terminal.Restore(0, oldState)
			panic(err)
		}
	}

}

type virtualConn struct {
	ssh.Channel
}

func (c *virtualConn) LocalAddr() net.Addr {
	return c
}

func (c *virtualConn) RemoteAddr() net.Addr {
	return c
}

func (c *virtualConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *virtualConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *virtualConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (c *virtualConn) String() string {
	return ""
}

func (c *virtualConn) Network() string {
	return "ssh"
}
