package main

import (
	"crypto/rand"
	"crypto/rsa"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"strings"
	"sync"

	"github.com/pkg/sftp"

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
		// Auth: []ssh.AuthMethod{
		// 	ssh.PublicKeys(
		// 		tryRSA(u),
		// 		tryDSA(u),
		// 	),
		// },
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
	signer, err := generateSigner()
	if err != nil {
		log.Fatal("Failed to generate host key", err)
	}

	go serveSFTP(signer)

	fsChan := client.HandleChannelOpen("localDirFs")
	go func() {
		for newChan := range fsChan {
			ch, reqs, err := newChan.Accept()
			if err != nil {
				panic(err)
			}
			go ssh.DiscardRequests(reqs)

			go func(in io.ReadWriteCloser) {
				defer in.Close()
				out, err := net.Dial("tcp", "localhost:2023")
				if err != nil {
					panic(err)
				}
				defer out.Close()

				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					io.Copy(in, out)
					wg.Done()
				}()
				go func() {
					io.Copy(out, in)
					wg.Done()
				}()
				wg.Wait()
			}(ch)

		}
	}()

	session, err := client.NewSession()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	session.SendRequest("env", false, ssh.Marshal(struct{ Key, Value string }{
		Key:   "CWD",
		Value: dir,
	}))

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	session.Stdin = os.Stdin

	cmd := flag.Args()[1:]
	if len(cmd) > 2 && cmd[0] == "--" {
		cmd = cmd[1:]
	}

	oldState, err := terminal.MakeRaw(0)
	if err != nil {
		panic(err)
	}

	if len(cmd) > 0 {
		if err := session.Start(strings.Join(cmd, " ")); err != nil {
			terminal.Restore(0, oldState)
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

		//defer terminal.Restore(0, oldState)
		// TODO: handle SIGWINCH
		if err := session.Shell(); err != nil {
			terminal.Restore(0, oldState)
			panic(err)
		}
	}

	if err := session.Wait(); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			terminal.Restore(0, oldState)
			os.Exit(exitErr.ExitStatus())
		} else {
			terminal.Restore(0, oldState)
			panic(err)
		}
	}
	terminal.Restore(0, oldState)

}

func generateSigner() (ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

func serveSFTP(signer ssh.Signer) {

	// An SSH server is represented by a ServerConfig, which holds
	// certificate details and handles authentication of ServerConns.
	config := &ssh.ServerConfig{
		NoClientAuth: true,
		// PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
		// 	if c.User() == "localfs" {
		// 		return nil, nil
		// 	}
		// 	return nil, fmt.Errorf("user rejected for %q", c.User())
		// },
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "0.0.0.0:2023")
	if err != nil {
		log.Fatal("failed to listen for connection", err)
	}
	//fmt.Printf("Listening on %v\n", listener.Addr())

	nConn, err := listener.Accept()
	if err != nil {
		log.Fatal("failed to accept incoming connection", err)
	}

	// Before use, a handshake must be performed on the incoming
	// net.Conn.
	_, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		log.Fatal("failed to handshake", err)
	}

	// The incoming Request channel must be serviced.
	go ssh.DiscardRequests(reqs)

	// Service the incoming Channel channel.
	for newChannel := range chans {
		// Channels have a type, depending on the application level
		// protocol intended. In the case of an SFTP session, this is "subsystem"
		// with a payload string of "<length=4>sftp"
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Fatal("could not accept channel.", err)
		}

		// Sessions have out-of-band requests such as "shell",
		// "pty-req" and "env".  Here we handle only the
		// "subsystem" request.
		go func(in <-chan *ssh.Request) {
			for req := range in {
				ok := false
				switch req.Type {
				case "subsystem":
					if string(req.Payload[4:]) == "sftp" {
						ok = true
					}
				}
				req.Reply(ok, nil)
			}
		}(requests)

		serverOptions := []sftp.ServerOption{}

		server, err := sftp.NewServer(
			channel,
			serverOptions...,
		)
		if err != nil {
			log.Fatal(err)
		}
		if err := server.Serve(); err == io.EOF {
			server.Close()
			log.Print("sftp client exited session.")
		} else if err != nil {
			log.Fatal("sftp server completed with error:", err)
		}
	}
}
