// Package httpjson provides facilities for transporting JSON encoded
// values HTTP message bodies.
package httpjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"
)

// IsJSONContentType returns whether the given Content-Type is a JSON MIME
// Type as defined by the WHATWG MIME Sniffing Standard section 4.6
// (https://mimesniff.spec.whatwg.org/#mime-type-groups).
func IsJSONContentType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// If it doesn't parse we can't say it's JSON.
		return false
	}
	if mt == "application/json" || mt == "text/json" || strings.HasSuffix(mt, "+json") {
		return true
	}
	return false
}

// MarshalRequest creates a new http.Request with the given method and URL
// and a body containing the JSON encoding of v.
//
// If v is nil then the request will have no body. Otherwise v will be
// marshaled and then encoded using the character set specified by
// contentType. If the contentType is empty then the default contentType of
// "application/json;charset=utf-8" is used. If the contentType doesn't
// specify a character set then the value will be encoded as "us-ascii".
//
// For a non-nil v the request will have the "Content-Length" and
// "Content-Type" headers set and include a GetBody method to support
// redirection.
func MarshalRequest(method, url, contentType string, v interface{}) (*http.Request, error) {
	if contentType == "" {
		contentType = `application/json;charset=utf-8`
	}
	var body []byte
	if v != nil {
		_, mtParam, _ := mime.ParseMediaType(contentType)
		var err error
		body, err = marshal(mtParam["charset"], v)
		if err != nil {
			return nil, err
		}
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", contentType)
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	return req, nil
}

// UnmarshalRequest parses the JSON-encoded body of an http.Request and
// stores the result in the value pointed to by v.
//
// UnmarshalRequest decodes the request body from the character set
// specified in the request's Content-Type header before parsing the JSON
// value.
func UnmarshalRequest(req *http.Request, v interface{}) error {
	buf, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	_, mtParam, _ := mime.ParseMediaType(req.Header.Get("Content-Type"))
	return unmarshal(buf, mtParam["charset"], v)
}

// WriteResponse writes the JSON encoding of v as the body of an HTTP
// response.
//
// The marshaled value will be encoded using the character set specified in
// the contentType. If the contentType is empty then the default
// contentType of "application/json;charset=utf-8" is used. If the
// contentType doesn't specify a character set then the value will be
// encoded as "us-ascii".
//
// If v is nil then WriteResponse will write an empty body, otherwise
// WriteResponse will set the Content-Length and Content-Type headers
// before writing the response body.
//
// If statusCode is > 0 then WriteResponse will call w.WriteHeader with the
// status code before writing the body.
func WriteResponse(w http.ResponseWriter, statusCode int, contentType string, v interface{}) error {
	if contentType == "" {
		contentType = "application/json;charset=utf-8"
	}
	var body []byte
	if v != nil {
		_, mtParam, _ := mime.ParseMediaType(contentType)
		var err error
		body, err = marshal(mtParam["charset"], v)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(body)), 10))
	}
	if statusCode > 0 {
		w.WriteHeader(statusCode)
	}
	_, err := w.Write(body)
	return err
}

// UnmarshalResponse parses the JSON-encoded body of an http.Response and
// stores the result in the value pointed to by v.
//
// UnmarshalResponse decodes the response body from the character set
// specified in the reponse's Content-Type header before parsing the JSON
// value.
func UnmarshalResponse(resp *http.Response, v interface{}) error {
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_, mtParam, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return unmarshal(buf, mtParam["charset"], v)
}

func marshal(charset string, v interface{}) ([]byte, error) {
	if charset == "" {
		// If the character-set isn't specified the default is us-ascii.
		charset = "us-ascii"
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(charset, "utf-8") {
		// The native format is "utf-8", there is no need to encode it.
		return buf, nil
	}
	enc, err := ianaindex.MIME.Encoding(charset)
	if err != nil {
		return nil, err
	}
	if enc == nil {
		return nil, errors.New("marshal: unsupported encoding")
	}
	encoder := &encoding.Encoder{
		Transformer: jsonTransformer{e: enc.NewEncoder()},
	}
	return encoder.Bytes(buf)
}

type jsonTransformer struct {
	e *encoding.Encoder
}

// Transform implements encoding.Transformer.
func (t jsonTransformer) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	for {
		nd, ns, err := t.e.Transformer.Transform(dst[nDst:], src[nSrc:], atEOF)
		nDst += nd
		nSrc += ns
		if _, ok := err.(replacementError); !ok {
			// Either the end of the input has been reached
			// (err == nil), or there is an error we can't
			// solve.
			return nDst, nSrc, err
		}
		// The only place in valid JSON that a non-ascii rune can
		// occur is in a string, so unicode escape the rune.
		r, ns := utf8.DecodeRune(src[nSrc:])
		if r == utf8.RuneError {
			// Can only get a rune error with a short src.
			return nDst, nSrc, transform.ErrShortSrc
		}
		buf := make([]byte, 6, 12)
		if r < 0x10000 {
			escape(buf[:], r)
		} else {
			buf = buf[:12]
			r1, r2 := utf16.EncodeRune(r)
			escape(buf[:6], r1)
			escape(buf[6:], r2)
		}
		nd, _, err = t.e.Transformer.Transform(dst[nDst:], buf[:], false)
		if err != nil {
			return nDst, nSrc, err
		}
		nDst += nd
		nSrc += ns
	}
}

const hexDigits = "0123456789abcdef"

func escape(b []byte, r rune) {
	b[0] = '\\'
	b[1] = 'u'
	b[2] = hexDigits[r>>12]
	b[3] = hexDigits[(r>>8)&0xf]
	b[4] = hexDigits[(r>>4)&0xf]
	b[5] = hexDigits[r&0xf]
}

// Reset implements encoding.Transformer.
func (t jsonTransformer) Reset() {}

// A replacementError is the error type that will be implemented by an
// encoding that doesn't include a particular rune.
type replacementError interface {
	Replacement() byte
}

func unmarshal(buf []byte, charset string, v interface{}) error {
	if charset != "" && !strings.EqualFold(charset, "utf-8") {
		enc, err := ianaindex.MIME.Encoding(charset)
		if err != nil {
			return err
		}
		if enc == nil {
			return errors.New("unmarshal: unsupported encoding")
		}
		buf, err = enc.NewDecoder().Bytes(buf)
		if err != nil {
			return err
		}
	}
	return json.Unmarshal(buf, v)
}
