package setup

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/env"
)

type profileTransport func(*http.Request) (*http.Response, error)

func (f profileTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Real TLS/object operations against an isolated XML/S3 fixture. No credentials
// or cloud accounts are required, and this is not a live-provider acceptance test.
func TestStorageProfilesUseExpectedSignedXMLRequests(t *testing.T) {
	for _, test := range []struct{ profile, location, host, region string }{
		{"aws", "us-west-2", "private-bucket.s3.us-west-2.amazonaws.com", "us-west-2"},
		{"digitalocean", "nyc3", "nyc3.digitaloceanspaces.com", "us-east-1"},
		{"google", "", "storage.googleapis.com", "auto"},
	} {
		t.Run(test.profile, func(t *testing.T) {
			objects := map[string]string{}
			seen := map[string]bool{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen[r.Method] = true
				if r.Host != test.host || !strings.Contains(r.Header.Get("Authorization"), "/"+test.region+"/s3/aws4_request") || (test.profile != "aws" && !strings.HasPrefix(r.URL.Path, "/private-bucket")) {
					t.Error("wrong endpoint, signing region or bucket addressing")
					http.Error(w, "invalid profile", 400)
					return
				}
				if r.URL.Query().Has("list-type") {
					type item struct{ Key string }
					type listing struct {
						XMLName  xml.Name `xml:"ListBucketResult"`
						Contents []item
					}
					result := listing{}
					for key := range objects {
						result.Contents = append(result.Contents, item{Key: strings.TrimPrefix(strings.TrimPrefix(key, "/private-bucket"), "/")})
					}
					w.Header().Set("Content-Type", "application/xml")
					xml.NewEncoder(w).Encode(result)
					return
				}
				switch r.Method {
				case "PUT":
					body, _ := io.ReadAll(r.Body)
					objects[r.URL.Path] = string(body)
					w.Header().Set("ETag", `"fixture"`)
				case "GET", "HEAD":
					body, ok := objects[r.URL.Path]
					if !ok {
						http.NotFound(w, r)
						return
					}
					w.Header().Set("ETag", `"fixture"`)
					w.Write([]byte(body))
				case "DELETE":
					delete(objects, r.URL.Path)
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			target, _ := url.Parse(server.URL)
			previous := http.DefaultTransport
			http.DefaultTransport = profileTransport(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				destination := *r.URL
				destination.Scheme = target.Scheme
				destination.Host = target.Host
				copy.URL = &destination
				copy.Host = r.URL.Host
				return server.Client().Transport.RoundTrip(copy)
			})
			defer func() { http.DefaultTransport = previous }()
			values := defaults()
			values["STORAGE_PROVIDER"] = "s3"
			values["THURA_STORAGE_PROFILE"] = test.profile
			values["THURA_STORAGE_LOCATION"] = test.location
			values["STORAGE_BUCKET"] = "private-bucket"
			values["STORAGE_PREFIX"] = "thura/isolated/"
			values["STORAGE_ACCESS_KEY"] = "PUBLIC-profile-access"
			values["STORAGE_SECRET_KEY"] = "PUBLIC-profile-secret"
			if err := configureStorageProfile(values, values, url.Values{"STORAGE_ACCESS_KEY": {"PUBLIC-profile-access"}, "STORAGE_SECRET_KEY": {"PUBLIC-profile-secret"}}); err != nil {
				t.Fatal(err)
			}
			var cfg storage.Config
			if err := env.Fill(&cfg, values); err != nil {
				t.Fatal(err)
			}
			client, err := storage.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err = client.Put(ctx, "probe.txt", strings.NewReader("round-trip"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			body, _, err := client.Get(ctx, "probe.txt")
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := io.ReadAll(body)
			body.Close()
			if string(payload) != "round-trip" {
				t.Fatal("read-back differs")
			}
			if _, err = client.Stat(ctx, "probe.txt"); err != nil {
				t.Fatal(err)
			}
			list, err := client.List(ctx, "", 10)
			if err != nil || len(list) != 1 || list[0].Key != "probe.txt" {
				t.Fatalf("listing failed: %v", err)
			}
			if err = client.Delete(ctx, "probe.txt"); err != nil {
				t.Fatal(err)
			}
			if len(objects) != 0 || !seen["PUT"] || !seen["GET"] || !seen["HEAD"] || !seen["DELETE"] {
				t.Fatal("incomplete private object round trip")
			}
		})
	}
}
