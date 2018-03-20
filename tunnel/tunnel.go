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
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/metadata"

	"github.com/Percona-Lab/pmm-api/agent"
	"github.com/Percona-Lab/pmm-api/managed"
)

type tunnel struct {
	dial     string
	listener net.Listener
	accepted chan *net.TCPConn
}

type Service struct {
	l *logrus.Entry

	rw      sync.RWMutex
	tunnels map[string][]tunnel // key is agent UUID
}

func NewService() *Service {
	return &Service{
		l:       logrus.WithField("component", "tunnel"),
		tunnels: make(map[string][]tunnel),
	}
}

func getAgentUUID(ctx context.Context) (string, error) {
	const h = "pmm-agent-uuid"
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md[h]) == 0 {
		return "", fmt.Errorf("no %s metadata", h)
	}
	if len(md[h]) > 1 {
		return "", fmt.Errorf("%s metadata has several values", h)
	}
	return strings.ToLower(md[h][0]), nil
}

func (s *Service) Make(stream agent.Tunnels_MakeServer) error {
	// TODO move to interceptor
	uuid, err := getAgentUUID(stream.Context())
	if err != nil {
		return err
	}

	var dial string
	var conn *net.TCPConn
	for {
		s.rw.RLock()
		tunnels := s.tunnels[uuid]
		s.rw.RUnlock()

		cases := make([]reflect.SelectCase, len(tunnels))
		for i, t := range tunnels {
			cases[i] = reflect.SelectCase{
				Dir:  reflect.SelectRecv,
				Chan: reflect.ValueOf(t.accepted),
			}
		}
		i, v, ok := reflect.Select(cases)
		if !ok {
			// channel is closed, we need to get a new list
			continue
		}

		dial = tunnels[i].dial
		conn = v.Interface().(*net.TCPConn)
		break
	}

	// send dial message
	err = stream.Send(&agent.TunnelsEnvelopeFromGateway{
		Payload: &agent.TunnelsEnvelopeFromGateway_DialRequest{
			DialRequest: &agent.TunnelsDialRequest{
				Dial: dial,
			},
		},
	})
	if err != nil {
		return err
	}

	// wait for dial response
	env, err := stream.Recv()
	if err != nil {
		return err
	}
	res := env.GetDialResponse()
	if res == nil {
		return fmt.Errorf("expected dial response, got %s", env)
	}
	if res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}

	var wg sync.WaitGroup

	// receive messages until error, write to TCP connection
	wg.Add(1)
	go func() {
		defer func() {
			conn.CloseWrite()
			wg.Done()
		}()

		for {
			env, recvErr := stream.Recv()
			if recvErr != nil {
				s.l.Error(recvErr)
				return
			}
			data := env.GetData()
			if data == nil {
				s.l.Errorf("Expected data, got %s.", env)
				return
			}

			if len(data.Data) != 0 {
				s.l.Debugf("Writing %d bytes...", len(data.Data))
				if _, writeErr := conn.Write(data.Data); writeErr != nil {
					s.l.Error(writeErr)
					return
				}
			}
			if data.Error != "" {
				s.l.Errorf("Got error, exiting: %s.", data.Error)
				return
			}
			if data.Closed {
				s.l.Info("Closed, exiting.")
				return
			}
		}
	}()

	// read from TCP connection until error, send messages
	wg.Add(1)
	go func() {
		defer func() {
			conn.CloseRead()
			wg.Done()
		}()

		for {
			b := make([]byte, 4096)
			n, readErr := conn.Read(b)
			s.l.Debugf("Read %d bytes, read error %v.", n, readErr)
			data := &agent.TunnelsData{
				Data: b[:n],
			}
			switch readErr {
			case nil:
				// nothing
			case io.EOF:
				s.l.Info("Read closed.")
				data.Closed = true
			default:
				s.l.Errorf("Failed to read: %s.", readErr)
				data.Closed = true
				data.Error = readErr.Error()
			}
			env := &agent.TunnelsEnvelopeFromGateway{
				Payload: &agent.TunnelsEnvelopeFromGateway_Data{
					Data: data,
				},
			}
			if err := stream.Send(env); err != nil {
				s.l.Errorf("Failed to send message: %s.", err)
				return
			}
			if data.Closed {
				return
			}
		}
	}()

	wg.Wait()
	return nil
}

func (s *Service) Create(ctx context.Context, req *managed.TunnelsCreateRequest) (*managed.TunnelsCreateResponse, error) {
	uuid := req.AgentUuid
	dial := req.Dial
	// TODO replace with validators
	if uuid == "" {
		return &managed.TunnelsCreateResponse{
			Error: "agent_uuid is not given",
		}, nil
	}
	if dial == "" {
		return &managed.TunnelsCreateResponse{
			Error: "dial is not given",
		}, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return &managed.TunnelsCreateResponse{
			Error: err.Error(),
		}, nil
	}

	accepted := make(chan *net.TCPConn)
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				s.l.Error(err)
				close(accepted)
				return
			}

			s.l.Debugf("Accepted connection on %s from %s to dial to %s:%s.", listener.Addr().String(), c.RemoteAddr().String(), uuid, dial)
			conn := c.(*net.TCPConn)
			conn.SetKeepAlivePeriod(20 * time.Second)
			conn.SetKeepAlive(true)
			// TODO SetReadBuffer, SetWriteBuffer?
			accepted <- conn
		}
	}()

	s.rw.Lock()
	tunnels := s.tunnels[uuid]
	tunnels = append(tunnels, tunnel{
		dial:     dial,
		listener: listener,
		accepted: accepted,
	})
	s.tunnels[uuid] = tunnels
	s.rw.Unlock()

	return &managed.TunnelsCreateResponse{
		Listen: listener.Addr().String(),
	}, nil
}

func (s *Service) Delete(ctx context.Context, req *managed.TunnelsDeleteRequest) (*managed.TunnelsDeleteResponse, error) {
	s.rw.Lock()
	defer s.rw.Unlock()

	for uuid, tunnels := range s.tunnels {
		for i, t := range tunnels {
			if t.listener.Addr().String() != req.Listen {
				continue
			}

			if err := t.listener.Close(); err != nil {
				s.l.Error(err)
			}
			// TODO close accepted connections
			tunnels = append(tunnels[:i], tunnels[i+1:]...)
			if len(tunnels) == 0 {
				delete(s.tunnels, uuid)
			} else {
				s.tunnels[uuid] = tunnels
			}
			return &managed.TunnelsDeleteResponse{}, nil
		}
	}

	return &managed.TunnelsDeleteResponse{
		Error: fmt.Sprintf("No tunnel with listen address %s.", req.Listen),
	}, nil
}

// check interfaces
var (
	_ agent.TunnelsServer   = (*Service)(nil)
	_ managed.TunnelsServer = (*Service)(nil)
)
