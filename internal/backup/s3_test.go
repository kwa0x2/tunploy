package backup

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Examples from the AWS "Signature Version 4 signing for Amazon S3" docs.
func TestSignMatchesAWSExamples(t *testing.T) {
	c := NewS3(S3Config{
		Endpoint:  "s3.amazonaws.com",
		Region:    "us-east-1",
		Bucket:    "examplebucket",
		AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	})
	at := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

	get, _ := c.request(context.Background(), http.MethodGet, "test.txt", nil, nil)
	get.Header.Set("Range", "bytes=0-9")
	c.sign(get, emptySHA256, at)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := get.Header.Get("Authorization"); got != want {
		t.Errorf("GET object\n got %s\nwant %s", got, want)
	}

	list, _ := c.request(context.Background(), http.MethodGet, "", url.Values{"max-keys": {"2"}, "prefix": {"J"}}, nil)
	c.sign(list, emptySHA256, at)
	if got := list.Header.Get("Authorization"); !strings.HasSuffix(got,
		"Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7") {
		t.Errorf("list objects: %s", got)
	}
}

func TestURLStyles(t *testing.T) {
	cases := []struct {
		cfg  S3Config
		want string
	}{
		{S3Config{Region: "eu-central-1", Bucket: "b"}, "https://b.s3.eu-central-1.amazonaws.com/tunploy/x%20y.tar.gz"},
		{S3Config{Endpoint: "http://minio:9000", Bucket: "b", PathStyle: true}, "http://minio:9000/b/tunploy/x%20y.tar.gz"},
		{S3Config{Endpoint: "https://acct.r2.cloudflarestorage.com/", Bucket: "b", PathStyle: true}, "https://acct.r2.cloudflarestorage.com/b/tunploy/x%20y.tar.gz"},
	}
	for _, tc := range cases {
		u, err := NewS3(tc.cfg).url("tunploy/x y.tar.gz")
		if err != nil || u.String() != tc.want {
			t.Errorf("url(%+v) = %v, %v; want %s", tc.cfg, u, err, tc.want)
		}
	}
}

// fakeS3 is a path-style bucket that checks each request is signed.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    int
}

func newFakeS3(t *testing.T) (*fakeS3, S3Config) {
	f := &fakeS3{objects: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, S3Config{Endpoint: srv.URL, Bucket: "bk", Prefix: "panel/", AccessKey: "ak", SecretKey: "sk", PathStyle: true}
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=ak/") || f.fail > 0 {
		if f.fail > 0 {
			f.fail--
		}
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
		return
	}
	key, ok := strings.CutPrefix(r.URL.Path, "/bk/")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<Error><Code>NoSuchBucket</Code></Error>`)
		return
	}
	switch {
	case r.Method == http.MethodGet && key == "":
		type content struct {
			Key          string
			Size         int
			LastModified time.Time
		}
		var out struct {
			XMLName  xml.Name `xml:"ListBucketResult"`
			Contents []content
		}
		for k, v := range f.objects {
			if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
				out.Contents = append(out.Contents, content{Key: k, Size: len(v), LastModified: time.Now()})
			}
		}
		xml.NewEncoder(w).Encode(out)
	case r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Amz-Content-Sha256") != hexSHA256(string(body)) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `<Error><Code>XAmzContentSHA256Mismatch</Code></Error>`)
			return
		}
		f.objects[key] = body
	case r.Method == http.MethodGet:
		body, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		w.Write(body)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeS3) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for k := range f.objects {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestS3RoundTrip(t *testing.T) {
	fake, cfg := newFakeS3(t)
	c := NewS3(cfg)
	ctx := context.Background()

	if err := c.Check(ctx); err != nil {
		t.Fatalf("check: %v", err)
	}
	if keys := fake.keys(); len(keys) != 0 {
		t.Fatalf("check left %v behind", keys)
	}

	body := []byte("archive")
	for _, name := range []string{FileName(time.Unix(100, 0), false), FileName(time.Unix(200, 0), false)} {
		if err := c.Put(ctx, c.Key(name), bytes.NewReader(body), int64(len(body)), hexSHA256(string(body))); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	fake.objects["panel/notes.txt"] = []byte("not a backup")

	list, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != FileName(time.Unix(200, 0), false) || list[0].Key != "panel/"+list[0].Name {
		t.Fatalf("list = %+v", list)
	}

	rc, _, err := c.Get(ctx, list[1].Key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "archive" {
		t.Fatalf("get = %q", got)
	}
	if _, _, err := c.Get(ctx, "panel/missing"); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("get missing = %v", err)
	}

	fake.fail = 1
	err = c.Check(ctx)
	var s3err *S3Error
	if !errors.As(err, &s3err) || s3err.Code != "AccessDenied" || !strings.Contains(err.Error(), "upload a test file") {
		t.Fatalf("check with denied access = %v", err)
	}
}
