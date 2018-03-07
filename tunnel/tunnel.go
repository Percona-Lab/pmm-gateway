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

package tunnel

import (
	"fmt"
	"net"
	"sync"

	"github.com/Percona-Lab/pmm-api/agent"
	"github.com/Percona-Lab/pmm-api/gateway"
	"github.com/sirupsen/logrus"
)

type Service struct {
	client agent.ServiceClient

	rw      sync.RWMutex
	tunnels map[string]net.Conn
}

func NewService(client agent.ServiceClient) *Service {
	return &Service{
		client:  client,
		tunnels: make(map[string]net.Conn),
	}
}

func (s *Service) runTunnel(c net.Conn, dial string) {
	defer c.Close()

	res, err := s.client.CreateTunnel(&agent.CreateTunnelRequest{
		Dial: dial,
	})
	if err != nil {
		logrus.Error(err)
		return
	}

	tunnelID := res.TunnelId

	s.rw.Lock()
	s.tunnels[tunnelID] = c
	s.rw.Unlock()

	for {
		b := make([]byte, 4096)
		n, err := c.Read(b)
		if err != nil {
			logrus.Error(err)
			return
		}
		if n == 0 {
			continue
		}

		res, err := s.client.WriteToTunnel(&agent.WriteToTunnelRequest{
			TunnelId: tunnelID,
			Data:     b[:n],
		})
		if err != nil {
			logrus.Error(err)
			return
		}
		if res.Error != "" {
			logrus.Error(res.Error)
			return
		}
	}
}

func (s *Service) CreateTunnel(req *gateway.CreateTunnelRequest) (*gateway.CreateTunnelResponse, error) {
	if req.AgentUuid != "" {
		return nil, fmt.Errorf("req.AgentUuid (%q) is not handled yet", req.AgentUuid)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return &gateway.CreateTunnelResponse{
			Error: err.Error(),
		}, nil
	}

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				logrus.Error(err)
				continue
			}
			go s.runTunnel(c, req.Dial)
		}
	}()

	return &gateway.CreateTunnelResponse{
		Listen: l.Addr().String(),
	}, nil
}

func (s *Service) WriteToTunnel(req *gateway.WriteToTunnelRequest) (*gateway.WriteToTunnelResponse, error) {
	s.rw.RLock()
	c := s.tunnels[req.TunnelId]
	s.rw.RUnlock()
	if c == nil {
		return &gateway.WriteToTunnelResponse{Error: fmt.Sprintf("no such tunnel: %s", req.TunnelId)}, nil
	}

	if _, err := c.Write(req.Data); err != nil {
		return &gateway.WriteToTunnelResponse{
			Error: err.Error(),
		}, nil
	}
	return &gateway.WriteToTunnelResponse{}, nil
}

// check interfaces
var _ gateway.ServiceServer = (*Service)(nil)
