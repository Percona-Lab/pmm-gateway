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
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Percona-Lab/wsrpc"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/Percona-Lab/pmm-api/agent"
	"github.com/Percona-Lab/pmm-api/gateway"
	"github.com/Percona-Lab/pmm-gateway/tunnel"
)

func handler(rw http.ResponseWriter, req *http.Request) {
	conn, err := wsrpc.Upgrade(rw, req)
	if err != nil {
		logrus.Error(err)
		http.Error(rw, err.Error(), 400)
		return
	}
	logrus.Infof("Connection from %s.", req.RemoteAddr)
	defer conn.Close()

	server := tunnel.NewService(agent.NewServiceClient(conn))

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINFO)
	go func() {
		const dial = "127.0.0.1:9100"
		<-signals
		logrus.Infof("Creating tunnel to %s", dial)
		res, err := server.CreateTunnel(&gateway.CreateTunnelRequest{
			Dial: dial,
		})
		if err != nil {
			logrus.Fatal(err)
		}
		logrus.Info(res)
	}()

	err = gateway.NewServiceDispatcher(conn, server).Run()
	logrus.Infof("Server exited with %v", err)
}

func main() {
	logrus.SetLevel(logrus.DebugLevel)
	kingpin.Parse()

	http.Handle("/", http.HandlerFunc(handler))
	const addr = "127.0.0.1:7781"
	logrus.Infof("Listening on %s...", addr)
	logrus.Fatal(http.ListenAndServe(addr, nil))
}
