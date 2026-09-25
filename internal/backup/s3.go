package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	emptySHA256   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	defaultRegion = "us-east-1"
)

var ErrObjectNotFound = errors.New("backup not found in the bucket")

// S3Config works with any S3-compatible service; an empty Endpoint means AWS.
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

type Object struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Encrypted bool      `json:"encrypted"`
}

type S3 struct {
	cfg    S3Config
	client *http.Client
	now    func() time.Time
}

func NewS3(cfg S3Config) *S3 {
	if cfg.Region == "" {
		cfg.Region = defaultRegion
	}
	return &S3{cfg: cfg, client: &http.Client{Timeout: 30 * time.Minute}, now: time.Now}
}

// S3Error carries the service's own explanation, which says far more than the status.
type S3Error struct {
	Status  int
	Code    string
	Message string
}

func (e *S3Error) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Code, e.Message, e.Status)
	case e.Code != "":
		return fmt.Sprintf("%s (HTTP %d)", e.Code, e.Status)
	default:
		return fmt.Sprintf("HTTP %d %s", e.Status, http.StatusText(e.Status))
	}
}

func (c *S3) Key(name string) string { return c.cfg.Prefix + name }

func (c *S3) Put(ctx context.Context, key string, body io.Reader, size int64, sha string) error {
	req, err := c.request(ctx, http.MethodPut, key, nil, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/gzip")
	resp, err := c.do(req, sha)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *S3) PutFile(ctx context.Context, key string, f *File) error {
	body, err := os.Open(f.Path)
	if err != nil {
		return err
	}
	defer body.Close()
	return c.Put(ctx, key, body, f.Size, f.SHA256)
}

// Check writes, lists and deletes a small file, so a wrong key or a missing
// permission shows up before the first real backup.
func (c *S3) Check(ctx context.Context) error {
	key := c.cfg.Prefix + ".tunploy-check"
	body := []byte("tunploy connection check\n")
	if err := c.Put(ctx, key, bytes.NewReader(body), int64(len(body)), hexSHA256(string(body))); err != nil {
		return fmt.Errorf("could not upload a test file: %w", err)
	}
	if _, err := c.List(ctx); err != nil {
		return fmt.Errorf("could not list files: %w", err)
	}
	if err := c.Delete(ctx, key); err != nil {
		return fmt.Errorf("could not delete the test file: %w", err)
	}
	return nil
}

// Get returns the object's body and size; the caller closes the body.
func (c *S3) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	req, err := c.request(ctx, http.MethodGet, key, nil, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.do(req, emptySHA256)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.ContentLength, nil
}

func (c *S3) Delete(ctx context.Context, key string) error {
	req, err := c.request(ctx, http.MethodDelete, key, nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req, emptySHA256)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// List returns the backups under the prefix, newest first.
func (c *S3) List(ctx context.Context) ([]Object, error) {
	var out []Object
	token := ""
	for {
		q := url.Values{"list-type": {"2"}, "prefix": {c.cfg.Prefix + filePrefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		req, err := c.request(ctx, http.MethodGet, "", q, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.do(req, emptySHA256)
		if err != nil {
			return nil, err
		}
		var page struct {
			Contents []struct {
				Key          string
				Size         int64
				LastModified time.Time
			}
			IsTruncated           bool
			NextContinuationToken string
		}
		err = xml.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read bucket listing: %w", err)
		}
		for _, o := range page.Contents {
			name := strings.TrimPrefix(o.Key, c.cfg.Prefix)
			if !IsFileName(name) {
				continue
			}
			out = append(out, Object{Key: o.Key, Name: name, Size: o.Size, Modified: o.LastModified, Encrypted: IsEncryptedName(name)})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			break
		}
		token = page.NextContinuationToken
	}
	// Names hold a UTC timestamp, so they sort by age.
	slices.SortFunc(out, func(a, b Object) int { return strings.Compare(b.Name, a.Name) })
	return out, nil
}

func (c *S3) request(ctx context.Context, method, key string, query url.Values, body io.Reader) (*http.Request, error) {
	u, err := c.url(key)
	if err != nil {
		return nil, err
	}
	u.RawQuery = canonicalQuery(query)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (c *S3) url(key string) (*url.URL, error) {
	endpoint := c.cfg.Endpoint
	if endpoint == "" {
		endpoint = "s3." + c.cfg.Region + ".amazonaws.com"
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("endpoint %q is not a valid URL", c.cfg.Endpoint)
	}
	p := "/" + key
	if c.cfg.PathStyle {
		p = "/" + c.cfg.Bucket + p
	} else {
		u.Host = c.cfg.Bucket + "." + u.Host
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + p
	u.RawPath = uriEncode(u.Path, false)
	return u, nil
}

func (c *S3) do(req *http.Request, payloadHash string) (*http.Response, error) {
	c.sign(req, payloadHash, c.now())
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	var body struct {
		Code    string
		Message string
	}
	xml.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	if resp.StatusCode == http.StatusNotFound && body.Code == "NoSuchKey" {
		return nil, ErrObjectNotFound
	}
	return nil, &S3Error{Status: resp.StatusCode, Code: body.Code, Message: body.Message}
}

// sign adds an AWS Signature Version 4 Authorization header.
func (c *S3) sign(req *http.Request, payloadHash string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	day := amzDate[:8]
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	headers := map[string]string{"host": req.URL.Host}
	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if lower == "range" || strings.HasPrefix(lower, "x-amz-") {
			headers[lower] = strings.TrimSpace(strings.Join(values, ","))
		}
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	slices.Sort(names)
	var canonHeaders strings.Builder
	for _, name := range names {
		canonHeaders.WriteString(name + ":" + headers[name] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")

	scope := day + "/" + c.cfg.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256(canonical)

	key := hmacSHA256([]byte("AWS4"+c.cfg.SecretKey), day)
	key = hmacSHA256(key, c.cfg.Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, toSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.cfg.AccessKey+"/"+scope+
		", SignedHeaders="+signed+", Signature="+signature)
}

func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range q[k] {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// uriEncode follows SigV4's rules, which differ from url.QueryEscape on spaces and '~'.
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case 'A' <= ch && ch <= 'Z', 'a' <= ch && ch <= 'z', '0' <= ch && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == '/' && !encodeSlash:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func hexSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
