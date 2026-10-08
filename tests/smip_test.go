package tests

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"thura/app"
	"thura/internal/federation"
	"thura/internal/smip"
	"thura/internal/workspace"
	"thura/schema"
)

type smipApp struct {
	server        *lidzatest.Server
	workspace     schema.Workspace
	owner, member auth.Profile
	password      string
	config        federation.Config
	private       ed25519.PrivateKey
}

func smipKeys(label string, now int64) (string, ed25519.PrivateKey, map[string]smip.Key) {
	seed := sha256.Sum256([]byte("PUBLIC SMIP adapter fixture " + label))
	private := ed25519.NewKeyFromSeed(seed[:])
	public := private.Public().(ed25519.PublicKey)
	return smip.Encode(seed[:]), private, map[string]smip.Key{smip.KeyID(public): {Public: public, NotBefore: now - 86400, NotAfter: now + 30*86400}}
}
func newSmipApp(t *testing.T, domain, peer string) *smipApp {
	t.Helper()
	now := time.Now().Unix()
	seed, private, _ := smipKeys(domain, now)
	_, _, peerKeys := smipKeys(peer, now)
	config := federation.Config{Domain: domain, Seed: seed, NotBefore: now - 86400, NotAfter: now + 30*86400, Peers: map[string]map[string]smip.Key{peer: peerKeys}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMIP_ENABLED", "true")
	t.Setenv("SMIP_CONFIG", string(raw))
	server := lidzatest.Start(t, app.New(nil))
	ctx := server.Context()
	password := "smip river meadow lantern 87334"
	stamp := time.Now().UnixNano()
	owner, err := auth.From(ctx).CreateUser(ctx, fmt.Sprintf("smip-owner-%d@example.com", stamp), "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	member, err := auth.From(ctx).CreateUser(ctx, fmt.Sprintf("smip-member-%d@example.com", stamp), "Member", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "SMIP fixture", OwnerEmail: owner.Email})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", member.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM smip_inbox WHERE workspace_id=$1", "DELETE FROM smip_binding WHERE workspace_id=$1", "DELETE FROM file_version WHERE file_id IN (SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM drive_folder WHERE workspace_id=$1", "DELETE FROM audit_event WHERE scope=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err := db.From(ctx).Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		if err := auth.From(ctx).DeleteUser(ctx, owner.Subject); err != nil {
			t.Error(err)
		}
		if err := auth.From(ctx).DeleteUser(ctx, member.Subject); err != nil {
			t.Error(err)
		}
	})
	return &smipApp{server: server, workspace: w, owner: owner, member: member, password: password, config: config, private: private}
}
func (f *smipApp) request(t *testing.T, method, path string, in, out any, want int) {
	t.Helper()
	if r := f.server.JSON(t, method, path, in, out); r.StatusCode != want {
		t.Fatalf("%s %s got %d want %d", method, path, r.StatusCode, want)
	}
}
func (f *smipApp) login(t *testing.T, p auth.Profile) {
	t.Helper()
	f.request(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: p.Email, Password: f.password}, nil, 200)
}
func (f *smipApp) base() string { return "/api/v1/workspaces/" + f.workspace.ID + "/smip" }
func (f *smipApp) binding(t *testing.T, peer, stream string) schema.SmipBinding {
	t.Helper()
	f.login(t, f.owner)
	var binding schema.SmipBinding
	in := schema.SmipBindingInput{RequestID: uuid.NewString(), Peer: peer, Stream: stream, Sender: "team@" + peer, Recipient: "team@" + f.config.Domain}
	f.request(t, "POST", f.base()+"/bindings", in, &binding, 200)
	return binding
}
func (f *smipApp) wire(t *testing.T, destination, stream, id, kind string, data []byte) smip.Packet {
	t.Helper()
	now := time.Now().Unix()
	e := smip.Envelope{Version: smip.Version, ID: id, From: f.config.Domain, To: destination, Sender: "team@" + f.config.Domain, Recipient: "team@" + destination, Stream: stream, Kind: kind, Created: now, Expires: now + 3600, Payload: smip.Encode(data)}
	if kind == "file" {
		e.Name = smip.Encode([]byte("receipt.bin"))
	}
	p, err := smip.Sign(e, f.private)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func smipTLS(t *testing.T, f *smipApp) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(f.server.Config.Handler)
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.Config.ReadTimeout = 30 * time.Second
	s.Config.WriteTimeout = 30 * time.Second
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}
func smipClient(t *testing.T, s *httptest.Server, f *smipApp) *smip.Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	public := f.private.Public().(ed25519.PublicKey)
	c, err := smip.NewApplicationClient(s.URL, f.config.Domain, map[string]smip.Key{smip.KeyID(public): {Public: public, NotBefore: f.config.NotBefore, NotAfter: f.config.NotAfter}}, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func smipFailure(t *testing.T, c *smip.Client, p smip.Packet, want int) {
	t.Helper()
	_, err := c.Send(context.Background(), p)
	status, ok := err.(*smip.HTTPError)
	if !ok || status.Status != want {
		t.Fatalf("SMIP response %v want %d", err, want)
	}
}

func TestSmipTwoAppServersConsentDurabilityAndReview(t *testing.T) {
	a := newSmipApp(t, "alpha.example", "beta.example")
	b := newSmipApp(t, "beta.example", "alpha.example")
	stream := uuid.NewString()
	a.binding(t, "beta.example", stream)
	// A valid peer signature does not authorize a workspace or unbound stream.
	c := smipClient(t, smipTLS(t, b), b)
	p := a.wire(t, b.config.Domain, stream, uuid.NewString(), "chat", []byte("<script>plain text</script>"))
	smipFailure(t, c, p, 403)
	binding := b.binding(t, "alpha.example", stream)
	var again schema.SmipBinding
	in := schema.SmipBindingInput{RequestID: binding.ID, Peer: binding.Peer, Stream: stream, Sender: binding.Sender, Recipient: binding.Recipient}
	b.request(t, "POST", b.base()+"/bindings", in, &again, 200)
	if again.ID != binding.ID {
		t.Fatal("binding retry duplicated consent")
	}
	in.Stream = uuid.NewString()
	b.request(t, "POST", b.base()+"/bindings", in, nil, 409)
	b.login(t, b.member)
	b.request(t, "POST", b.base()+"/bindings", in, nil, 403)
	b.request(t, "DELETE", b.base()+"/bindings/"+binding.ID, nil, nil, 403)
	first, err := c.Send(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := c.Send(context.Background(), p)
	if err != nil || duplicate != first {
		t.Fatal("duplicate acknowledgement changed", err)
	}
	var list schema.SmipInboxList
	b.request(t, "GET", b.base()+"/inbox", nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].Body != "<script>plain text</script>" || list.Items[0].Digest != p.Digest() {
		t.Fatal("review lost content or provenance")
	}
	b.request(t, "GET", b.base()+"/inbox?cursor=invalid", nil, nil, 422)
	// Restart the adapter while retaining only the application's database.
	restarted, err := federation.NewGateway(b.config)
	if err != nil {
		t.Fatal(err)
	}
	restartServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		restarted.ServeHTTP(w, req.WithContext(lidza.WithServices(req.Context(), b.server.Services)))
	}))
	restartServer.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	restartServer.Config.ReadHeaderTimeout = 5 * time.Second
	restartServer.Config.ReadTimeout = 30 * time.Second
	restartServer.Config.WriteTimeout = 30 * time.Second
	restartServer.StartTLS()
	t.Cleanup(restartServer.Close)
	restartedClient := smipClient(t, restartServer, b)
	recovered, err := restartedClient.Send(context.Background(), p)
	if err != nil || recovered != first {
		t.Fatal("database receipt lost across adapter restart", err)
	}

	// Bidirectional acceptance through the second actual application router.
	reverse := smipClient(t, smipTLS(t, a), a)
	back := b.wire(t, a.config.Domain, stream, uuid.NewString(), "chat", []byte("response"))
	if _, err := reverse.Send(context.Background(), back); err != nil {
		t.Fatal(err)
	}
	b.login(t, b.owner)
	b.request(t, "DELETE", b.base()+"/bindings/"+binding.ID, nil, nil, 204)
	duplicate, err = c.Send(context.Background(), p)
	if err != nil || duplicate != first {
		t.Fatal("disabled consent lost accepted receipt", err)
	}
	smipFailure(t, c, a.wire(t, b.config.Domain, stream, uuid.NewString(), "chat", []byte("new")), 403)
	// No caller can use another workspace's cursor or session as permission.
	b.request(t, "GET", "/api/v1/workspaces/"+a.workspace.ID+"/smip/inbox", nil, nil, 404)
	if _, err = db.From(b.server.Context()).Exec(b.server.Context(), "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", b.member.Subject, b.workspace.ID); err != nil {
		t.Fatal(err)
	}
	b.login(t, b.member)
	b.request(t, "GET", b.base()+"/inbox", nil, nil, 404)
}

func TestSmipFileExplicitImportQuotaRevocationAndIdempotency(t *testing.T) {
	a := newSmipApp(t, "sender.example", "receiver.example")
	b := newSmipApp(t, "receiver.example", "sender.example")
	stream := uuid.NewString()
	b.binding(t, a.config.Domain, stream)
	c := smipClient(t, smipTLS(t, b), b)
	data := bytes.Repeat([]byte{0, 255, 7, 19}, (10<<20)/4)
	p := a.wire(t, b.config.Domain, stream, uuid.NewString(), "file", data)
	if _, err := c.Send(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	var list schema.SmipInboxList
	b.request(t, "GET", b.base()+"/inbox", nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].ImportedFileID != nil || list.Items[0].Size != len(data) || list.Items[0].Name != "receipt.bin" {
		t.Fatal("transport acceptance imported a file or lost metadata")
	}
	var files schema.DriveListing
	b.request(t, "GET", "/api/v1/workspaces/"+b.workspace.ID+"/drive", nil, &files, 200)
	if len(files.Files) != 0 {
		t.Fatal("transport acceptance silently wrote Drive")
	}
	importPath := b.base() + "/inbox/" + list.Items[0].ID + "/import"
	t.Setenv("DRIVE_QUOTA_BYTES", "1048576")
	b.request(t, "POST", importPath, schema.SmipImportInput{}, nil, 409)
	b.request(t, "POST", importPath, schema.SmipImportInput{FolderID: &a.workspace.ID}, nil, 404)
	t.Setenv("DRIVE_QUOTA_BYTES", "1073741824")
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var file schema.DriveFile
			b.request(t, "POST", importPath, schema.SmipImportInput{}, &file, 200)
			ids <- file.ID
		}()
	}
	wg.Wait()
	close(ids)
	fileID := ""
	for id := range ids {
		if fileID != "" && fileID != id {
			t.Fatal("concurrent import duplicated file")
		}
		fileID = id
	}
	var content schema.FileContent
	b.request(t, "GET", "/api/v1/workspaces/"+b.workspace.ID+"/drive/files/"+fileID+"/content", nil, &content, 200)
	decoded, err := base64.StdEncoding.DecodeString(content.Data)
	if err != nil || !bytes.Equal(decoded, data) || content.ContentType != "application/octet-stream" {
		t.Fatal("import changed exact binary bytes", err)
	}
	var count int
	if err = db.From(b.server.Context()).QueryRow(b.server.Context(), "SELECT COUNT(*) FROM file_version WHERE file_id=$1", fileID).Scan(&count); err != nil || count != 1 {
		t.Fatal("import duplicated versions", err)
	}
	b.request(t, "GET", b.base()+"/inbox", nil, &list, 200)
	if list.Items[0].ImportedAt == nil || list.Items[0].ImportedFileID == nil || *list.Items[0].ImportedFileID != fileID {
		t.Fatal("import status lost")
	}
	if _, err = db.From(b.server.Context()).Exec(b.server.Context(), "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", b.member.Subject, b.workspace.ID); err != nil {
		t.Fatal(err)
	}
	b.login(t, b.member)
	b.request(t, "POST", importPath, schema.SmipImportInput{}, nil, 404)
	smipFailure(t, c, a.wire(t, b.config.Domain, stream, uuid.NewString(), "file", append(data, 0)), 403)
	bad := p.Envelope
	bad.ID = uuid.NewString()
	bad.Name = smip.Encode([]byte("../unsafe.bin"))
	unsafe, err := smip.Sign(bad, a.private)
	if err != nil {
		t.Fatal(err)
	}
	smipFailure(t, c, unsafe, 403)
}

func TestSmipImportFailuresRollbackAndRecover(t *testing.T) {
	a := newSmipApp(t, "rollback-sender.example", "rollback-receiver.example")
	root := filepath.Join(t.TempDir(), "objects")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STORAGE_PROVIDER", "local")
	t.Setenv("STORAGE_DIR", root)
	b := newSmipApp(t, "rollback-receiver.example", "rollback-sender.example")
	stream := uuid.NewString()
	b.binding(t, a.config.Domain, stream)
	c := smipClient(t, smipTLS(t, b), b)
	p := a.wire(t, b.config.Domain, stream, uuid.NewString(), "file", []byte{0, 255, 14, 23})
	if _, err := c.Send(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	var list schema.SmipInboxList
	b.request(t, "GET", b.base()+"/inbox", nil, &list, 200)
	path := b.base() + "/inbox/" + list.Items[0].ID + "/import"
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("storage unavailable"), 0600); err != nil {
		t.Fatal(err)
	}
	b.request(t, "POST", path, schema.SmipImportInput{}, nil, 500)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root+"-moved", root); err != nil {
		t.Fatal(err)
	}
	var count int
	ctx := b.server.Context()
	pool := db.From(ctx)
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM drive_file WHERE workspace_id=$1", b.workspace.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("storage failure committed file metadata", err)
	}
	// Audit persistence is part of import, so a failure rolls back both the file
	// version and imported marker. A possible object is left to grace-aware GC.
	constraint := fmt.Sprintf("smip_audit_fixture_%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, fmt.Sprintf("ALTER TABLE audit_event ADD CONSTRAINT %s CHECK(action<>'smip.file.import' OR scope<>'%s')", constraint, b.workspace.ID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "ALTER TABLE audit_event DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	b.request(t, "POST", path, schema.SmipImportInput{}, nil, 500)
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM drive_file WHERE workspace_id=$1", b.workspace.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("audit failure committed file metadata", err)
	}
	b.request(t, "GET", b.base()+"/inbox", nil, &list, 200)
	if list.Items[0].ImportedAt != nil || list.Items[0].ImportedFileID != nil {
		t.Fatal("failed import marked completion")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE audit_event DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	var file schema.DriveFile
	b.request(t, "POST", path, schema.SmipImportInput{}, &file, 200)
	// Corrupt signed records fail before transport receipt recovery/import.
	if _, err := pool.Exec(ctx, "UPDATE smip_inbox SET record='{}' WHERE id=$1", list.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	smipFailure(t, c, p, 503)
	if _, err := pool.Exec(ctx, "UPDATE smip_inbox SET imported_file_id=NULL,imported_at=NULL WHERE id=$1", list.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	b.request(t, "POST", path, schema.SmipImportInput{}, nil, 500)
}

func TestSmipDisabledReceiverAndAnonymousReview(t *testing.T) {
	t.Setenv("SMIP_ENABLED", "false")
	t.Setenv("SMIP_CONFIG", "")
	server := lidzatest.Start(t, app.New(nil))
	if response := server.JSON(t, "POST", smip.ApplicationMessagePath, map[string]string{}, nil); response.StatusCode != 503 {
		t.Fatalf("disabled receiver got %d", response.StatusCode)
	}
	if response := server.JSON(t, "GET", "/api/v1/workspaces/"+uuid.NewString()+"/smip/inbox", nil, nil); response.StatusCode != 401 {
		t.Fatalf("anonymous review got %d", response.StatusCode)
	}
}

func TestSmipMetadataPagingAndRetainedByteQuota(t *testing.T) {
	a := newSmipApp(t, "quota-sender.example", "quota-receiver.example")
	b := newSmipApp(t, "quota-receiver.example", "quota-sender.example")
	stream := uuid.NewString()
	b.binding(t, a.config.Domain, stream)
	c := smipClient(t, smipTLS(t, b), b)
	for i := 0; i < 51; i++ {
		p := a.wire(t, b.config.Domain, stream, uuid.NewString(), "chat", []byte("page fixture"))
		if _, err := c.Send(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	var first, second schema.SmipInboxList
	b.request(t, "GET", b.base()+"/inbox", nil, &first, 200)
	if len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatal("first review page was not bounded")
	}
	b.request(t, "GET", b.base()+"/inbox?cursor="+first.NextCursor, nil, &second, 200)
	if len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatal("cursor lost final accepted packet")
	}
	seen := map[string]bool{}
	for _, item := range first.Items {
		seen[item.ID] = true
	}
	if seen[second.Items[0].ID] {
		t.Fatal("cursor repeated a packet")
	}
	data := bytes.Repeat([]byte{0, 255}, (10<<20)/2)
	var accepted smip.Packet
	var receipt smip.Receipt
	for i := 0; i < 6; i++ {
		accepted = a.wire(t, b.config.Domain, stream, uuid.NewString(), "file", data)
		var err error
		receipt, err = c.Send(context.Background(), accepted)
		if err != nil {
			t.Fatal(err)
		}
	}
	tooMuch := a.wire(t, b.config.Domain, stream, uuid.NewString(), "file", data[:5<<20])
	smipFailure(t, c, tooMuch, 503)
	recovered, err := c.Send(context.Background(), accepted)
	if err != nil || recovered != receipt {
		t.Fatal("capacity prevented recovery of an existing receipt", err)
	}
	// The ordinary review response must never return retained binary packets.
	b.request(t, "GET", b.base()+"/inbox", nil, &first, 200)
	raw, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"payload"`)) || bytes.Contains(raw, []byte(`"record"`)) || len(raw) > 1<<20 {
		t.Fatal("review materialized retained file payloads")
	}
}
