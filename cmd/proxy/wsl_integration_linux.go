/*
Copyright © 2024 SUSE LLC
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/rancher-sandbox/rancher-desktop-networking/pkg/portproxy"
	"github.com/rancher-sandbox/rancher-desktop-networking/pkg/utils"
)

var (
	debug        bool
	upstreamAddr string
	listenAddr   string
)

const (
	k8sAPI            = "192.168.1.2:6443"
	defaultListenAddr = "127.0.0.1:6443"
)

func main() {
	flag.BoolVar(&debug, "debug", false, "enable additional debugging.")
	flag.StringVar(&upstreamAddr, "upstream-addr", k8sAPI, "The upstream server's address (k3s API sever).")
	flag.StringVar(&listenAddr, "listen-addr", defaultListenAddr, "The server's address in an IP:PORT format.")
	flag.Parse()

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// TODO: we can also need to remove this static proxy
	// for k8s API because guest agent will register this port upon start up
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		logrus.Fatalf("Failed to listen on %s: %s", listenAddr, err)
	}

	//	socket, err := net.Listen("unix", "/run/wsl-proxy.sock")
	//	if err != nil {
	//		logrus.Errorf("failed to create listener for published ports: %s", err)
	//	}
	proxy, err := portproxy.NewPortProxy("/run/wsl-proxy.sock")
	if err != nil {
		logrus.Errorf("failed to create listener for published ports: %s", err)
	}

	// quit := make(chan struct{})
	// go publishedPortsListener(quit, socket)
	go proxy.Listen()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// WaitGroup to wait for all connections to finish before shutting down
	var wg sync.WaitGroup

	go func() {
		<-sigCh
		logrus.Println("Shutting down...")
		proxy.Close()
		listener.Close()
		wg.Wait()
		proxy.Wait()
		os.Exit(0)
	}()

	logrus.Infof("Proxy server started listening on %s, forwarding to %s", listenAddr, upstreamAddr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Check if the error is due to listener being closed
			if errors.Is(err, net.ErrClosed) {
				break
			}
			logrus.Errorf("Failed to accept listener: %s", err)
			continue
		}
		logrus.Debugf("Accepted connection from %s", conn.RemoteAddr())

		wg.Add(1)

		go func(conn net.Conn) {
			defer wg.Done()
			defer conn.Close()
			utils.Pipe(conn, upstreamAddr)
		}(conn)
	}
}
