// pmm-gateway
// Copyright (C) 2018 Percona LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"context"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Percona-Lab/pmm-api/agent"
	"github.com/Percona-Lab/pmm-api/managed"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/Percona-Lab/pmm-gateway/tunnel"
)

// func handler(rw http.ResponseWriter, req *http.Request) {
// 	conn, err := wsrpc.Upgrade(rw, req, nil)
// 	if err != nil {
// 		logrus.Error(err)
// 		http.Error(rw, err.Error(), 400)
// 		return
// 	}
// 	logrus.Infof("Connection from %s.", req.RemoteAddr)
// 	defer conn.Close()

// 	server := tunnel.NewService(agent.NewServiceClient(conn))

// 	signals := make(chan os.Signal, 1)
// 	signal.Notify(signals, syscall.SIGINFO)
// 	go func() {
// 		const dial = "127.0.0.1:9100"
// 		<-signals
// 		logrus.Infof("Creating tunnel to %s", dial)
// 		res, err := server.CreateTunnel(&gateway.CreateTunnelRequest{
// 			Dial: dial,
// 		})
// 		if err != nil {
// 			logrus.Fatal(err)
// 		}
// 		logrus.Info(res)
// 	}()

// 	err = gateway.NewServiceDispatcher(conn, server).Run()
// 	logrus.Infof("Server exited with %v", err)
// }

const (
	shutdownTimeout = 3 * time.Second
)

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(0)
	log.SetPrefix("stdlog: ")
	logrus.SetLevel(logrus.DebugLevel)
	kingpin.Parse()

	l := logrus.WithField("component", "main")
	defer l.Info("Done.")
	ctx, cancel := context.WithCancel(context.Background())

	// handle termination signals
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-signals
		signal.Stop(signals)
		l.Warnf("Got %v (%d) signal, shutting down...", s, s)
		cancel()
	}()

	gRPCServer := grpc.NewServer()
	tunnels := tunnel.NewService()
	agent.RegisterTunnelsServer(gRPCServer, tunnels)
	managed.RegisterTunnelsServer(gRPCServer, tunnels)

	// TODO FIXME remove this hack for demo
	{
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINFO)
		go func() {
			const dial = "127.0.0.1:9100"
			<-signals
			logrus.Infof("Creating tunnel to %s", dial)
			res, err := tunnels.Create(context.TODO(), &managed.TunnelsCreateRequest{
				Dial: dial,
			})
			if err != nil {
				logrus.Fatal(err)
			}
			logrus.Info(res)
		}()
	}

	const addr = "127.0.0.1:8080"
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		l.Panic(err)
	}
	go func() {
		for {
			err = gRPCServer.Serve(listener)
			if err == nil || err == grpc.ErrServerStopped {
				break
			}
			l.Errorf("Failed to serve: %s", err)
		}
		l.Info("Server stopped.")
	}()

	<-ctx.Done()
	ctx, cancel = context.WithTimeout(context.Background(), shutdownTimeout)
	go func() {
		<-ctx.Done()
		gRPCServer.Stop()
	}()
	gRPCServer.GracefulStop()
	cancel()
}
