package mailabuse

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
)

func TestStructuralLinksAndImpersonation(t *testing.T) {
	people := []Person{{Name: "Jane Doe", Email: "jane@company.example"}}
	for _, tc := range []struct{ name, from, text, html, code string }{{"name impersonation", "Jane Doe <attacker@other.example>", "hello", "", "name_impersonation"}, {"lookalike domain", "joe@cornpany.example", "", "", "lookalike_sender"}, {"private link", "real@sender.example", "http://127.0.0.1/secret", "", "ip_link"}, {"disguised target", "real@sender.example", "", `<a href="https://evil.example/pay">https://company.example/pay</a>`, "misleading_link"}, {"URL credentials", "real@sender.example", "https://company.example@evil.example/pay", "", "link_credentials"}, {"script URL", "real@sender.example", "", `<a href="javascript:alert(1)">Open</a>`, "unsafe_link_scheme"}} {
		t.Run(tc.name, func(t *testing.T) {
			e := Analyze(tc.from, tc.text, tc.html, people, nil)

			found := false
			for _, f := range e.Findings {
				found = found || f.Code == tc.code
			}
			if !found || e.Score < 80 {
				t.Fatalf("missing finding: %+v", e)
			}
		})
	}
	ordinary := Analyze("Jane Doe <jane@company.example>", "See https://company.example/notes", "", people, nil)
	if ordinary.Score >= 70 {
		t.Fatal("ordinary link flagged", ordinary)
	}
	if identity("a@example.org", true) == identity("a@example.org", false) {
		t.Fatal("unverified identity inherited SMIP trust")
	}
}
func TestAttachmentContentsAndArchiveBounds(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("word/vbaProject.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("macro")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []File{{Name: "invoice.pdf.exe", Data: []byte("MZpayload")}, {Name: "invoice.pdf", Data: []byte("%PDF-1.5 /JavaScript malicious")}, {Name: "innocent.docx", Data: archive.Bytes()}, {Name: "photo\u202egpj.exe", Data: []byte("script")}} {
		if e := Analyze("sender@example.org", "", "", nil, []File{file}); e.Score < 80 {
			t.Fatal("dangerous attachment not quarantined", file.Name, e)
		}
	}
	if e := Analyze("sender@example.org", "", "", nil, []File{{Name: "notes.txt", Data: []byte("hello")}}); e.Score != 0 {
		t.Fatal("plain attachment marked dangerous", e)
	}
}
func TestClamdBoundedProtocolAndFailures(t *testing.T) {
	for _, reply := range []string{"stream: OK\x00", "stream: Synthetic.Test FOUND\x00", "INSTREAM size limit exceeded. ERROR\x00", "partial"} {
		t.Run(reply, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan []byte, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				header := make([]byte, 10)
				if _, err = io.ReadFull(conn, header); err != nil {
					return
				}
				if string(header) != "zINSTREAM\x00" {
					return
				}
				var body bytes.Buffer
				for {
					var size uint32
					if binary.Read(conn, binary.BigEndian, &size) != nil {
						return
					}
					if size == 0 {
						break
					}
					if _, err = io.CopyN(&body, conn, int64(size)); err != nil {
						return
					}
				}
				done <- body.Bytes()
				if _, err = io.WriteString(conn, reply); err != nil {
					return
				}
			}()
			data := bytes.Repeat([]byte("x"), 100000)
			state, err := ScanContent(context.Background(), listener.Addr().String(), data)
			if !bytes.Equal(<-done, data) {
				t.Fatal("stream bytes changed")
			}
			switch {
			case reply == "stream: OK\x00":
				if err != nil || state != "clean" {
					t.Fatal(state, err)
				}
			case strings.Contains(reply, "FOUND"):
				if err != nil || state != "infected" {
					t.Fatal(state, err)
				}
			default:
				if err == nil {
					t.Fatal("incomplete scan accepted")
				}
			}
		})
	}
}
