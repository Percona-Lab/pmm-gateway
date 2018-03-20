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
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Percona-Lab/pmm-api/agent"
	"github.com/Percona-Lab/pmm-api/managed"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/Percona-Lab/pmm-gateway/frontend"
	"github.com/Percona-Lab/pmm-gateway/tunnel"
)

const (
	shutdownTimeout = 3 * time.Second
)

func gRPCServer() *grpc.Server {
	server := grpc.NewServer()
	server = grpc.NewServer()
	tunnels := tunnel.NewService()
	agent.RegisterTunnelsServer(server, tunnels)
	managed.RegisterTunnelsServer(server, tunnels)

	// TODO FIXME remove this hack for demo
	{
		const uuid = "baf4e293-9f1c-4b3f-9244-02c8f3f37d9d"
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINFO)
		go func() {
			const dial = "127.0.0.1:9100"
			<-signals
			logrus.Infof("Creating tunnel to %s", dial)
			res, err := tunnels.Create(context.TODO(), &managed.TunnelsCreateRequest{
				AgentUuid: uuid,
				Dial:      dial,
			})
			if err != nil {
				logrus.Fatal(err)
			}
			logrus.Infof("%+v", res)
		}()
	}

	return server
}

func runServer(ctx context.Context, addr string) {
	l := logrus.WithField("component", "server")
	l.Infof("Starting on %s...", addr)
	defer l.Info("Done.")

	gRPCServer := gRPCServer()
	frontend := frontend.NewService()
	handler := func(rw http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.Header.Get("Content-Type"), "application/grpc") {
			gRPCServer.ServeHTTP(rw, req)
		} else {
			frontend.ServeHTTP(rw, req)
		}
	}

	server := http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(handler),

		// TLSConfig: &tls.Config{
		// 	Certificates: []tls.Certificate{*cert},
		// 	NextProtos:   []string{"h2"},
		// },

		ReadTimeout:       10 * time.Minute,
		ReadHeaderTimeout: 5 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       5 * time.Minute,
		ErrorLog:          log.New(os.Stderr, "server: ", log.Flags()),
	}

	go func() {
		err := server.ListenAndServe()
		if err == http.ErrServerClosed {
			l.Info(err)
		} else {
			l.Error(err)
		}
	}()

	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	go gRPCServer.GracefulStop()
	if err := server.Shutdown(ctx); err != nil {
		l.Error(err)
	}

	gRPCServer.Stop()
	if err := server.Close(); err != nil {
		l.Error(err)
	}
}

func runDebugServer(ctx context.Context, addr string) {
	l := logrus.WithField("component", "debug")
	l.Infof("Starting on %s...", addr)
	defer l.Info("Done.")
}

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

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		runServer(ctx, "127.0.0.1:8080")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		runDebugServer(ctx, "127.0.0.1:8081")
	}()

	wg.Wait()
}
