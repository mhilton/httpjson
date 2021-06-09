package httpjson

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

// DefaultClient is the client used by Get and Do.
var DefaultClient = &Client{}

// Get retrieves a JSON document from the given URL and unmarshals the
// value into v. If the HTTP request results in a valid response that is
// not a success the resulting error will be of type *ResponseError.
func Get(ctx context.Context, url string, v interface{}) error {
	return DefaultClient.Get(ctx, url, v)
}

// Do creates and sends an HTTP request and processes the response. The
// request has the given method and is addressed to url, if req is not nil
// then it will be JSON encoded and used as the request body. The content
// type of the request is specified by contentType, which defaults to
// "application/json;charset=utf-8". If the HTTP request results in a valid
// response that is not a success the resulting error will be of type
// *ResponseError.
func Do(ctx context.Context, method, url, contentType string, req, resp interface{}) error {
	return DefaultClient.Do(ctx, method, url, contentType, req, resp)
}

// A Client is an HTTP client that transports JSON-encoded bodies. It's
// zero value (DefaultClient) is a usable client that uses
// http.DefaultClient.
type Client struct {
	// HTTPClient is the http.Client to use for all HTTP requests. If
	// this is nil http.DefaultClient is used.
	HTTPClient *http.Client

	// IsJSONContentType is used to determine if an HTTP response
	// contains a JSON-encoded body. If this is nil the
	// IsJSONContentType function is used.
	IsJSONContentType func(contentType string) bool
}

// Get retrieves a JSON document from the given URL and unmarshals the
// value into v. If the HTTP request results in a valid response that is
// not a success the resulting error will be of type *ResponseError.
func (c *Client) Get(ctx context.Context, url string, v interface{}) error {
	return c.Do(ctx, "GET", url, "", nil, v)
}

// Do creates and sends an HTTP request and processes the response. The
// request has the given method and is addressed to url, if req is not nil
// then it will be JSON encoded and used as the request body. The content
// type of the request is specified by contentType, which defaults to
// "application/json;charset=utf-8". If the HTTP request results in a valid
// response that is not a success the resulting error will be of type
// *ResponseError.
func (c *Client) Do(ctx context.Context, method, url, contentType string, req, resp interface{}) error {
	hreq, err := MarshalRequest(method, url, contentType, req)
	if err != nil {
		return err
	}
	hreq = hreq.WithContext(ctx)
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	hresp, err := client.Do(hreq)
	if err != nil {
		return err
	}
	defer hresp.Body.Close()

	if !(200 <= hresp.StatusCode && hresp.StatusCode < 300) {
		return newResponseError(hresp)
	}

	isJSONContentType := c.IsJSONContentType
	if isJSONContentType == nil {
		isJSONContentType = IsJSONContentType
	}
	if !isJSONContentType(hresp.Header.Get("Content-Type")) {
		return fmt.Errorf("unsupported Content-Type %q", hresp.Header.Get("Content-Type"))
	}
	return UnmarshalResponse(hresp, resp)
}

// A ResponseError is the error returned when the HTTP request returns a
// valid response that is either not a successful response, or is not a
// JSON content type.
type ResponseError struct {
	// Response contains the http.Response that caused the error. The
	// Body field of this object will be nil and should be read from
	// the error's Body field.
	Response *http.Response

	// Body contains the body of the http Response that caused the
	// error.
	Body []byte
}

// Error implements error.
func (e *ResponseError) Error() string {
	// Attempt to use a text body as an error message.
	mt, params, err := mime.ParseMediaType(e.Response.Header.Get("Content-Type"))
	if err == nil && strings.HasPrefix(mt, "text/") {
		buf := e.Body
		charset := params["charset"]
		if charset != "" && !strings.EqualFold(charset, "utf-8") {
			var enc encoding.Encoding
			enc, err = ianaindex.MIME.Encoding(charset)
			if err == nil && enc != nil {
				buf, err = enc.NewDecoder().Bytes(buf)
			}
		}
		if err == nil && len(buf) > 0 && len(buf) < 256 {
			return string(bytes.TrimSpace(buf))
		}
	}
	return e.Response.Status
}

// newResponseError creates a new ResponseError containing resp.
func newResponseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp1 := *resp
	resp1.Body = nil
	return &ResponseError{
		Response: &resp1,
		Body:     body,
	}
}
