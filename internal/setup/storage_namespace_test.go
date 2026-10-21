package setup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/agim/lidza/packs/storage"
)

func TestInstallationPrefixesSeparateIdenticalObjectKeysInOneBucket(t *testing.T) {
	var mu sync.Mutex
	objects := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read failed", 500)
				return
			}
			objects[r.URL.Path] = string(body)
			w.Header().Set("ETag", `"fixture"`)
		case http.MethodGet:
			body, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, body)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	for _, id := range []string{"10000000-0000-4000-8000-000000000001", "10000000-0000-4000-8000-000000000002"} {
		store, err := storage.New(storage.Config{Provider: "s3", Bucket: "shared", Endpoint: server.URL, Prefix: "thura/" + id + "/", AccessKey: "synthetic", SecretKey: "synthetic"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Put(context.Background(), "drive/same-key.txt", strings.NewReader(id), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		r, _, err := store.Get(context.Background(), "drive/same-key.txt")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(r)
		r.Close()
		if err != nil || string(body) != id {
			t.Fatal("read escaped installation namespace")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(objects) != 2 {
		t.Fatal("identical object keys collided in shared bucket")
	}
}
