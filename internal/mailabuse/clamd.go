package mailabuse

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

// ScanContent implements clamd's bounded INSTREAM protocol. The operator
// controls the endpoint; no message-provided URL or local filename is opened.
func ScanContent(ctx context.Context, endpoint string, data []byte) (string, error) {
	if len(data) > 20<<20 {
		return "", errors.New("message exceeds antivirus stream limit")
	}
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return "", errors.New("antivirus service unavailable")
	}
	defer connection.Close()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = connection.SetDeadline(deadline); err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() {
		if err := connection.Close(); err != nil {
			slog.DebugContext(ctx, "close cancelled antivirus connection", "error", err)
		}
	})
	defer stop()
	if _, err = io.WriteString(connection, "zINSTREAM\x00"); err != nil {
		return "", err
	}
	for remaining := data; len(remaining) > 0; {
		n := len(remaining)
		if n > 64<<10 {
			n = 64 << 10
		}
		if err = binary.Write(connection, binary.BigEndian, uint32(n)); err != nil {
			return "", err
		}
		if _, err = connection.Write(remaining[:n]); err != nil {
			return "", err
		}
		remaining = remaining[n:]
	}
	if err = binary.Write(connection, binary.BigEndian, uint32(0)); err != nil {
		return "", err
	}
	reply, err := bufio.NewReader(io.LimitReader(connection, 4097)).ReadString(0)
	if err != nil || len(reply) > 4096 {
		return "", errors.New("incomplete antivirus result")
	}
	reply = strings.TrimSuffix(reply, "\x00")
	if reply == "stream: OK" {
		return "clean", nil
	}
	if strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND") {
		return "infected", nil
	}
	return "", errors.New("antivirus could not fully inspect this attachment")
}
