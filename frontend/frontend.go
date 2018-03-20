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

package frontend

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

type Service struct {
	m             *http.ServeMux
	transport     *http.Transport
	flushInterval time.Duration
	l             *log.Logger
}

func makeURL(u string) *url.URL {
	res, err := url.Parse(u)
	if err != nil {
		panic(err)
	}
	return res
}

func (s *Service) withLogger(f func(*http.Request)) func(*http.Request) {
	return func(req *http.Request) {
		before := req.URL.String()
		f(req)
		s.l.Printf("%s -> %s", before, req.URL.String())
	}
}

func (s *Service) prometheus(req *http.Request) {
	s.proxyPass(req, makeURL("http://127.0.0.1:9090/"))
}

func (s *Service) managed(req *http.Request) {
	s.stripPathPrefix(req, "/managed")
	s.proxyPass(req, makeURL("http://127.0.0.1:7772/"))
}

func (s *Service) handle(location string, director func(*http.Request)) {
	s.m.Handle(location, &httputil.ReverseProxy{
		Director:      s.withLogger(director),
		Transport:     s.transport,
		FlushInterval: s.flushInterval,
		ErrorLog:      s.l,
	})
}

func NewService() *Service {
	// TODO make httputil.BufferPool?

	s := &Service{
		m: http.NewServeMux(),
		transport: &http.Transport{
			MaxIdleConnsPerHost: 50,
		},
		flushInterval: 100 * time.Millisecond,
		l:             log.New(os.Stderr, "frontend: ", log.Flags()),
	}

	s.handle("/prometheus/", s.prometheus)
	s.handle("/managed/", s.managed)

	return s
}

func (s *Service) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	s.m.ServeHTTP(rw, req)
}

// check interfaces
var _ http.Handler = (*Service)(nil)
