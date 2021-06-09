package httpjson_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/mhilton/httpjson"
)

func TestDo(t *testing.T) {
	srv := httptest.NewServer(echoHandler)
	defer srv.Close()

	var req, resp testValue
	req.S = "test message ☺"
	err := httpjson.Do(context.Background(), "POST", srv.URL, "", req, &resp)
	qt.Assert(t, err, qt.IsNil)
	qt.Check(t, resp.S, qt.Equals, "test message ☺")
}

func TestDoMarshalError(t *testing.T) {
	srv := httptest.NewServer(echoHandler)
	defer srv.Close()

	var req, resp testValue
	req.S = "test message ☺"
	err := httpjson.Do(context.Background(), "POST", srv.URL, "application/json;charset=made-up", req, &resp)
	qt.Check(t, err, qt.ErrorMatches, `ianaindex: invalid encoding name`)
}

func TestDoResponseError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	var req, resp testValue
	req.S = "test message ☺"
	err := httpjson.Do(context.Background(), "POST", srv.URL, "", req, &resp)
	qt.Check(t, err, qt.ErrorMatches, `404 page not found`)
}

func TestDoResponseErrorCharset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain;charset=iso-8859-1")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte{0xa3, '\n'})
	}))
	defer srv.Close()

	var req, resp testValue
	req.S = "test message ☺"
	err := httpjson.Do(context.Background(), "POST", srv.URL, "", req, &resp)
	qt.Check(t, err, qt.ErrorMatches, `£`)
}

func TestDoResponseErrorNoMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var req, resp testValue
	req.S = "test message ☺"
	err := httpjson.Do(context.Background(), "POST", srv.URL, "", req, &resp)
	qt.Check(t, err, qt.ErrorMatches, `500 Internal Server Error`)
}

func TestDoBadContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not JSON content"))
	}))
	defer srv.Close()

	var req, resp testValue
	req.S = "test message ☺"
	err := httpjson.Do(context.Background(), "POST", srv.URL, "", req, &resp)
	qt.Check(t, err, qt.ErrorMatches, `unsupported Content-Type "text/plain; charset=utf-8"`)
}

func TestClientDo(t *testing.T) {
	srv := httptest.NewTLSServer(echoHandler)
	defer srv.Close()
	cl := httpjson.Client{
		HTTPClient: srv.Client(),
	}

	var req, resp testValue
	req.S = "test message ☺"
	err := cl.Do(context.Background(), "POST", srv.URL, "", req, &resp)
	qt.Assert(t, err, qt.IsNil)
	qt.Check(t, resp.S, qt.Equals, "test message ☺")
}

func TestClientDoCustomContentType(t *testing.T) {
	srv := httptest.NewTLSServer(echoHandler)
	defer srv.Close()
	cl := httpjson.Client{
		HTTPClient: srv.Client(),
		IsJSONContentType: func(contentType string) bool {
			return contentType == "x-application/test;charset=utf-8"
		},
	}

	var req, resp testValue
	req.S = "test message ☺"
	err := cl.Do(context.Background(), "POST", srv.URL, "x-application/test;charset=utf-8", req, &resp)
	qt.Assert(t, err, qt.IsNil)
	qt.Check(t, resp.S, qt.Equals, "test message ☺")
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(valueHandler{v: testValue{S: "test message ☺"}})
	defer srv.Close()

	var resp testValue
	err := httpjson.Get(context.Background(), srv.URL, &resp)
	qt.Assert(t, err, qt.IsNil)
	qt.Check(t, resp.S, qt.Equals, "test message ☺")
}

func TestClientGet(t *testing.T) {
	srv := httptest.NewTLSServer(valueHandler{v: testValue{S: "test message ☺"}})
	defer srv.Close()
	cl := httpjson.Client{
		HTTPClient: srv.Client(),
	}

	var resp testValue
	err := cl.Get(context.Background(), srv.URL, &resp)
	qt.Assert(t, err, qt.IsNil)
	qt.Check(t, resp.S, qt.Equals, "test message ☺")
}

var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
	var v interface{}
	if err := httpjson.UnmarshalRequest(req, &v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.WriteResponse(w, http.StatusOK, req.Header.Get("Content-Type"), v)
})

type valueHandler struct {
	v interface{}
}

func (h valueHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	httpjson.WriteResponse(w, http.StatusOK, "", h.v)
}
