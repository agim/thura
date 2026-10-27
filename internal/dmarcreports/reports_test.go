package dmarcreports

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const feedback = `<feedback><report_metadata><report_id>fixture-1</report_id></report_metadata><policy_published><domain>sender.example</domain></policy_published><record><row><source_ip>203.0.113.7</source_ip></row></record></feedback>`

func reportMessage(subject, name string, data []byte) []byte {
	return []byte(fmt.Sprintf("From: postmaster@tenant.example\r\nTo: reports@collector.example\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=report\r\n\r\n--report\r\nContent-Type: text/plain\r\n\r\nAggregate email authentication counts.\r\n--report\r\nContent-Type: application/gzip; name=\"%s\"\r\nContent-Disposition: attachment; filename=\"%s\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--report--\r\n", subject, name, name, base64.StdEncoding.EncodeToString(data)))
}
func TestAggregateValidationAcceptsXMLAndGzipWithoutOriginalEmailContent(t *testing.T) {
	var zipped bytes.Buffer
	w := gzip.NewWriter(&zipped)
	w.Write([]byte(feedback))
	w.Close()
	for name, data := range map[string][]byte{"report.xml": []byte(feedback), "report.xml.gz": zipped.Bytes()} {
		parsed, err := Validate(reportMessage("Report domain: sender.example", name, data), []string{"reports@collector.example"})
		if err != nil || len(parsed.Attachments) != 1 {
			t.Fatal(name, err)
		}
	}
	for _, raw := range [][]byte{reportMessage("Ordinary private message", "report.xml", []byte(feedback)), reportMessage("Report domain: sender.example", "secret.txt", []byte("private email")), reportMessage("Report domain: sender.example", "report.xml", []byte(`<feedback><message>private original</message></feedback>`))} {
		if _, err := Validate(raw, []string{"reports@collector.example"}); err == nil {
			t.Fatal("accepted a non-aggregate message")
		}
	}
	if _, err := Validate(reportMessage("Report domain: sender.example", "report.xml", []byte(feedback)), []string{"forged\r\nBcc: victim@example.com"}); err == nil {
		t.Fatal("accepted injected recipient")
	}
}
func TestReportCompressionAndRecipientLimits(t *testing.T) {
	var zipped bytes.Buffer
	w := gzip.NewWriter(&zipped)
	w.Write([]byte(strings.Repeat("x", 5<<20)))
	w.Close()
	if _, err := Validate(reportMessage("Report domain: sender.example", "report.xml.gz", zipped.Bytes()), []string{"reports@collector.example"}); err == nil {
		t.Fatal("accepted oversized expansion")
	}
	if _, err := Validate(reportMessage("Report domain: sender.example", "report.xml", []byte(feedback)), make([]string, 11)); err == nil {
		t.Fatal("accepted excess report recipients")
	}
}
