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
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeRequest(t *testing.T, method, url string, body io.ReadCloser) *http.Request {
	req, err := http.NewRequest(method, url, body)
	require.NoError(t, err)
	return req
}

func TestPrometheus(t *testing.T) {
	s := NewService()

	req := makeRequest(t, "GET", "/prometheus/targets", nil)
	s.prometheus(req)
	assert.Equal(t, "http://127.0.0.1:9090/prometheus/targets", req.URL.String())
}

func TestManaged(t *testing.T) {
	s := NewService()
	req := makeRequest(t, "GET", "/managed/v1/version", nil)
	s.managed(req)
	assert.Equal(t, "http://127.0.0.1:7772/v1/version", req.URL.String())
}
