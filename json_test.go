package httpjson_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	qt "github.com/frankban/quicktest"

	"github.com/mhilton/httpjson"
)

var isJSONContentTypeTests = []struct {
	contentType string
	isJSON      bool
}{
	{"", false},
	{"application/json", true},
	{"application/something+json", true},
	{"text/json", true},
	{"text/plain", false},
	{`application/json;charset="ebcdic"`, true},
}

func TestIsJSONContentType(t *testing.T) {
	for _, test := range isJSONContentTypeTests {
		if httpjson.IsJSONContentType(test.contentType) != test.isJSON {
			t.Errorf("IsJSONContentType(%q) expected %v, got %v", test.contentType, test.isJSON, !test.isJSON)
		}
	}
}

var marshalRequestTests = []struct {
	name              string
	method            string
	url               string
	contentType       string
	v                 interface{}
	expectError       string
	expectBody        []byte
	expectContentType string
}{{
	name:              "simple",
	method:            "POST",
	url:               "https://test.example.com",
	v:                 testValue{S: "☺"},
	expectBody:        []byte(`{"s":"☺"}`),
	expectContentType: "application/json;charset=utf-8",
}, {
	name:   "nil",
	method: "GET",
	url:    "https://test.example.com",
}, {
	name:              "us-ascii",
	method:            "POST",
	url:               "https://test.example.com",
	contentType:       "application/json",
	v:                 testValue{S: "☺"},
	expectBody:        []byte(`{"s":"\u263a"}`),
	expectContentType: "application/json",
}, {
	name:              "wide_escape",
	method:            "POST",
	url:               "https://test.example.com",
	contentType:       "application/json",
	v:                 testValue{S: "😂 hello"},
	expectBody:        []byte(`{"s":"\ud83d\ude02 hello"}`),
	expectContentType: "application/json",
}, {
	name:              "iso-8859-1",
	method:            "POST",
	url:               "https://test.example.com",
	contentType:       "application/json;charset=iso-8859-1",
	v:                 testValue{S: "£☺"},
	expectBody:        []byte("{\"s\":\"\xa3\\u263a\"}"),
	expectContentType: "application/json;charset=iso-8859-1",
}, {
	name:        "unknown_charset",
	method:      "POST",
	url:         "https://test.example.com",
	contentType: "application/json;charset=no-such",
	v:           testValue{S: "☺"},
	expectError: `ianaindex: invalid encoding name`,
}, {
	name:        "unsupported_charset",
	method:      "POST",
	url:         "https://test.example.com",
	contentType: "application/json;charset=OSD_EBCDIC_DF03_IRV",
	v:           testValue{S: "☺"},
	expectError: `marshal: unsupported encoding`,
}, {
	name:        "unmarshalable_value",
	method:      "POST",
	url:         "https://test.example.com",
	contentType: "application/json;charset=OSD_EBCDIC_DF03_IRV",
	v: struct {
		C chan int `json:"c"`
	}{C: nil},
	expectError: `json: unsupported type: chan int`,
}, {
	name:        "invalid_url",
	method:      "POST",
	url:         ":::",
	contentType: "",
	v:           testValue{S: "☺"},
	expectError: `parse ":::": missing protocol scheme`,
}}

func TestMarshalRequest(t *testing.T) {
	for _, test := range marshalRequestTests {
		t.Run(test.name, func(t *testing.T) {
			req, err := httpjson.MarshalRequest(test.method, test.url, test.contentType, test.v)
			if test.expectError != "" {
				qt.Check(t, err, qt.ErrorMatches, test.expectError)
				return
			}
			qt.Assert(t, err, qt.IsNil)
			qt.Check(t, req.ContentLength, qt.Equals, int64(len(test.expectBody)))
			qt.Check(t, req.Header.Get("Content-Type"), qt.Equals, test.expectContentType)
			if test.expectBody == nil {
				qt.Check(t, req.Body, qt.IsNil)
				qt.Check(t, req.GetBody, qt.IsNil)
				return
			}
			qt.Assert(t, req.Body, qt.Not(qt.IsNil))
			qt.Check(t, req.GetBody, qt.Not(qt.IsNil))
			buf, err := io.ReadAll(req.Body)
			qt.Assert(t, err, qt.IsNil)
			qt.Check(t, string(buf), qt.Equals, string(test.expectBody))
		})
	}
}

func TestMarshalRequestGetBody(t *testing.T) {
	req, err := httpjson.MarshalRequest("POST", "https://test.example.com", "", testValue{S: "☺"})
	qt.Assert(t, err, qt.IsNil)
	body, err := req.GetBody()
	qt.Assert(t, err, qt.IsNil)
	buf, err := io.ReadAll(body)
	qt.Assert(t, err, qt.IsNil)
	qt.Check(t, string(buf), qt.Equals, `{"s":"☺"}`)
}

var unmarshalRequestTests = []struct {
	name        string
	contentType string
	body        io.Reader
	expectError string
	expectValue interface{}
}{{
	name:        "utf-8",
	contentType: "application/json;charset=utf-8",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectValue: testValue{S: "☺"},
}, {
	name:        "us-ascii",
	contentType: "application/json;charset=us-ascii",
	body:        strings.NewReader(`{"s":"\u263a"}`),
	expectValue: testValue{S: "☺"},
}, {
	name:        "iso-8859-1",
	contentType: "application/json;charset=iso-8859-1",
	body:        strings.NewReader("{\"s\":\"\\u263a\xa3\"}"),
	expectValue: testValue{S: "☺£"},
}, {
	name:        "unspecified_charset_utf-8",
	contentType: "application/json",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectValue: testValue{S: "☺"},
}, {
	name:        "unknown_charset",
	contentType: "application/json;charset=not-known",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectError: `ianaindex: invalid encoding name`,
}, {
	name:        "unsupported_charset",
	contentType: "application/json;charset=OSD_EBCDIC_DF03_IRV",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectError: `unmarshal: unsupported encoding`,
}, {
	name:        "bad_json",
	contentType: "application/json;charset=utf-8",
	body:        strings.NewReader("{"),
	expectError: `unexpected end of JSON input`,
}, {
	name:        "read_error",
	contentType: "application/json;charset=utf-8",
	body:        iotest.ErrReader(errors.New("test error")),
	expectError: `test error`,
}}

func TestUnmarshalRequest(t *testing.T) {
	for _, test := range unmarshalRequestTests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "https://test.example.com", test.body)
			qt.Assert(t, err, qt.IsNil)
			req.Header.Set("Content-Type", test.contentType)
			var v json.RawMessage
			err = httpjson.UnmarshalRequest(req, &v)
			if test.expectError != "" {
				qt.Check(t, err, qt.ErrorMatches, test.expectError)
				return
			}
			qt.Assert(t, err, qt.IsNil)
			qt.Check(t, []byte(v), qt.JSONEquals, test.expectValue)
		})
	}
}

var writeReponseTests = []struct {
	name              string
	code              int
	contentType       string
	v                 interface{}
	expectError       string
	expectStatusCode  int
	expectContentType string
	expectBody        []byte
}{{
	name:              "no_contentType",
	v:                 testValue{S: "☺"},
	expectStatusCode:  http.StatusOK,
	expectContentType: "application/json;charset=utf-8",
	expectBody:        []byte(`{"s":"☺"}`),
}, {
	name:              "us-ascii",
	code:              http.StatusCreated,
	contentType:       "application/json;charset=us-ascii",
	v:                 testValue{S: "☺"},
	expectStatusCode:  http.StatusCreated,
	expectContentType: "application/json;charset=us-ascii",
	expectBody:        []byte(`{"s":"\u263a"}`),
}, {
	name:             "no_content",
	code:             http.StatusNoContent,
	expectStatusCode: http.StatusNoContent,
	expectBody:       nil,
}, {
	name:        "unknown_charset",
	contentType: "application/json;charset=no-such",
	v:           testValue{S: "☺"},
	expectError: `ianaindex: invalid encoding name`,
}, {
	name:        "unsupported_charset",
	contentType: "application/json;charset=OSD_EBCDIC_DF03_IRV",
	v:           testValue{S: "☺"},
	expectError: `marshal: unsupported encoding`,
}, {
	name: "unmarshalable_value",
	v: struct {
		C chan int `json:"c"`
	}{C: nil},
	expectError: `json: unsupported type: chan int`,
}}

func TestWriteResponse(t *testing.T) {
	for _, test := range writeReponseTests {
		t.Run(test.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			err := httpjson.WriteResponse(rr, test.code, test.contentType, test.v)
			if test.expectError != "" {
				qt.Check(t, err, qt.ErrorMatches, test.expectError)
				return
			}
			qt.Assert(t, err, qt.IsNil)
			resp := rr.Result()
			qt.Check(t, resp.StatusCode, qt.Equals, test.expectStatusCode)
			body, err := io.ReadAll(resp.Body)
			qt.Assert(t, err, qt.IsNil)
			if test.expectBody == nil {
				qt.Check(t, resp.Header.Get("Content-Type"), qt.Equals, "")
				qt.Check(t, len(body), qt.Equals, 0)
				return
			}
			qt.Check(t, resp.Header.Get("Content-Type"), qt.Equals, test.expectContentType)
			qt.Check(t, int(resp.ContentLength), qt.Equals, len(test.expectBody))
			qt.Check(t, string(body), qt.Equals, string(test.expectBody))
		})
	}
}

var unmarshalResponseTests = []struct {
	name        string
	contentType string
	body        io.Reader
	expectError string
	expectValue interface{}
}{{
	name:        "utf-8",
	contentType: "application/json;charset=utf-8",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectValue: testValue{S: "☺"},
}, {
	name:        "us-ascii",
	contentType: "application/json;charset=us-ascii",
	body:        strings.NewReader(`{"s":"\u263a"}`),
	expectValue: testValue{S: "☺"},
}, {
	name:        "iso-8859-1",
	contentType: "application/json;charset=iso-8859-1",
	body:        strings.NewReader("{\"s\":\"\\u263a\xa3\"}"),
	expectValue: testValue{S: "☺£"},
}, {
	name:        "unspecified_charset_utf-8",
	contentType: "application/json",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectValue: testValue{S: "☺"},
}, {
	name:        "unknown_charset",
	contentType: "application/json;charset=not-known",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectError: `ianaindex: invalid encoding name`,
}, {
	name:        "unsupported_charset",
	contentType: "application/json;charset=OSD_EBCDIC_DF03_IRV",
	body:        strings.NewReader(`{"s":"☺"}`),
	expectError: `unmarshal: unsupported encoding`,
}, {
	name:        "bad_json",
	contentType: "application/json;charset=utf-8",
	body:        strings.NewReader("{"),
	expectError: `unexpected end of JSON input`,
}, {
	name:        "read_error",
	contentType: "application/json;charset=utf-8",
	body:        iotest.ErrReader(errors.New("test error")),
	expectError: `test error`,
}}

func TestUnmarshalResponse(t *testing.T) {
	for _, test := range unmarshalResponseTests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				Header: http.Header{
					"Content-Type": []string{test.contentType},
				},
				Body: io.NopCloser(test.body),
			}
			var v json.RawMessage
			err := httpjson.UnmarshalResponse(resp, &v)
			if test.expectError != "" {
				qt.Check(t, err, qt.ErrorMatches, test.expectError)
				return
			}
			qt.Assert(t, err, qt.IsNil)
			qt.Check(t, []byte(v), qt.JSONEquals, test.expectValue)
		})
	}
}

type testValue struct {
	S string `json:"s"`
}
